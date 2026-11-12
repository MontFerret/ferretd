package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
)

func sessionResponse(name string) *executionv1.Session {
	return &executionv1.Session{Id: &executionv1.SessionId{Value: "new-session"}, Source: &executionv1.SourceSnapshot{WorkspaceId: &workspacev1.WorkspaceId{Value: "workspace"}}, RuntimeName: name}
}

func TestSessionRuntimeCompatibilityAndAcknowledgment(t *testing.T) {
	for _, test := range []struct {
		name                 string
		minor                uint32
		selected, returned   string
		unsupported, invalid bool
	}{
		{name: "older unselected", minor: 1},
		{name: "catalog unselected", minor: 2},
		{name: "older explicit native", minor: 2, selected: "local", unsupported: true},
		{name: "older explicit application", minor: 1, selected: "application", unsupported: true},
		{name: "resolved default", minor: 3, returned: "application"},
		{name: "explicit", minor: 3, selected: "application", returned: "application"},
		{name: "future", minor: 99, selected: "application", returned: "application"},
		{name: "missing default", minor: 3, invalid: true},
		{name: "missing acknowledgment", minor: 3, selected: "application", invalid: true},
		{name: "mismatched acknowledgment", minor: 3, selected: "application", returned: "local", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, closes := 0, 0
			cleanupErr := errors.New("cleanup failed")
			operation, cancel := context.WithCancel(t.Context())
			defer cancel()
			c := &ExecutionClient{serverAPI: APIVersion{Major: 1, Minor: test.minor}, client: &executionClientStub{
				createSession: func(_ context.Context, request *executionv1.CreateSessionRequest) (*executionv1.CreateSessionResponse, error) {
					calls++

					if request.RuntimeName != test.selected {
						t.Fatalf("selector = %q", request.RuntimeName)
					}

					if test.invalid {
						cancel()
					}

					return &executionv1.CreateSessionResponse{Session: sessionResponse(test.returned)}, nil
				},
				closeSession: func(ctx context.Context, request *executionv1.CloseSessionRequest) (*executionv1.CloseSessionResponse, error) {
					closes++

					deadline, ok := ctx.Deadline()
					if ctx.Err() != nil || !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) <= 0 || request.Id.Value != "new-session" {
						t.Fatalf("invalid cleanup context or ID: %v, %+v", ctx.Err(), request)
					}

					return nil, cleanupErr
				},
			}}
			got, err := c.CreateSession(operation, CreateSessionRequest{WorkspaceID: "workspace", RelativePath: "query.fql", RuntimeName: test.selected})
			switch {
			case test.unsupported:
				if !errors.Is(err, ErrUnsupportedOperation) || calls != 0 || closes != 0 {
					t.Fatalf("unsupported selection = %+v, %v, calls %d, closes %d", got, err, calls, closes)
				}
			case test.invalid:
				if !errors.Is(err, errInvalidSessionRuntime) || !errors.Is(err, cleanupErr) || got.ID != "" || calls != 1 || closes != 1 {
					t.Fatalf("invalid acknowledgment = %+v, %v, calls %d, closes %d", got, err, calls, closes)
				}
			default:
				if err != nil || got.RuntimeName != test.returned || calls != 1 || closes != 0 {
					t.Fatalf("CreateSession = %+v, %v", got, err)
				}
			}
		})
	}
}

func TestGetSessionRuntimeValidationDoesNotClose(t *testing.T) {
	for _, minor := range []uint32{1, 2, 3} {
		c := &ExecutionClient{serverAPI: APIVersion{Major: 1, Minor: minor}, client: &executionClientStub{
			getSession: func(context.Context, *executionv1.GetSessionRequest) (*executionv1.GetSessionResponse, error) {
				return &executionv1.GetSessionResponse{Session: sessionResponse("")}, nil
			},
			closeSession: func(context.Context, *executionv1.CloseSessionRequest) (*executionv1.CloseSessionResponse, error) {
				t.Fatal("lookup closed a Session")

				return nil, nil
			},
		}}

		got, err := c.GetSession(t.Context(), "existing")
		if minor < 3 && (err != nil || got.RuntimeName != "") {
			t.Fatalf("older response = %+v, %v", got, err)
		}

		if minor == 3 && !errors.Is(err, errInvalidSessionRuntime) {
			t.Fatalf("new response error = %v", err)
		}
	}
}

func TestRuntimeErrorsRemainDistinct(t *testing.T) {
	for _, test := range []struct {
		condition executionv1.ResourceCondition
		code      codes.Code
		want      error
	}{
		{executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND, codes.NotFound, ErrRuntimeNotFound},
		{executionv1.ResourceCondition_RESOURCE_CONDITION_UNAVAILABLE, codes.Unavailable, ErrRuntimeUnavailable},
		{executionv1.ResourceCondition_RESOURCE_CONDITION_UNAVAILABLE, codes.DeadlineExceeded, context.DeadlineExceeded},
	} {
		rpc, err := status.New(test.code, "application failed").WithDetails(&executionv1.ResourceErrorDetail{Resource: executionv1.ResourceKind_RESOURCE_KIND_RUNTIME, Condition: test.condition})
		if err != nil {
			t.Fatal(err)
		}

		mapped := mapCreateSessionError(t.Context(), rpc.Err())
		if !errors.Is(mapped, test.want) || errors.Is(mapped, ErrDaemonUnavailable) || errors.Is(mapped, ErrExecutionSourceNotFound) || status.Code(mapped) != test.code {
			t.Fatalf("runtime error = %v", mapped)
		}
	}

	base, err := status.New(codes.InvalidArgument, "remote compiler metadata unavailable").WithDetails(&executionv1.CompilationFailure{})
	if err != nil {
		t.Fatal(err)
	}

	mapped := mapCreateSessionError(t.Context(), base.Err())

	var compilation *CompilationError
	if !errors.As(mapped, &compilation) || !errors.Is(mapped, ErrCompilationFailed) || !strings.Contains(mapped.Error(), "remote compiler metadata unavailable") {
		t.Fatalf("compilation error = %v", mapped)
	}
}

func TestNegotiatedVersionIsRetainedWithoutPerRequestInfo(t *testing.T) {
	for _, minor := range []uint32{1, 2, 3} {
		t.Run(fmt.Sprint(minor), func(t *testing.T) {
			server, connection := newSessionVersionServer(t, minor)

			request := CreateSessionRequest{WorkspaceID: "workspace", RelativePath: "query.fql"}
			if _, err := connection.Executions().CreateSession(t.Context(), request); err != nil {
				t.Fatal(err)
			}

			server.minor.Store(99)
			// A later informational query must not change the initial negotiation.
			if _, err := connection.Info(t.Context()); err != nil {
				t.Fatal(err)
			}

			request.RuntimeName = "local"

			_, err := connection.Executions().CreateSession(t.Context(), request)
			if minor < 3 && (!errors.Is(err, ErrUnsupportedOperation) || server.creates.Load() != 1) {
				t.Fatalf("old server selector = %v, creates %d", err, server.creates.Load())
			}

			if minor == 3 && (err != nil || server.creates.Load() != 2) {
				t.Fatalf("selector = %v, creates %d", err, server.creates.Load())
			}

			if server.infos.Load() != 2 {
				t.Fatalf("information calls = %d", server.infos.Load())
			}
		})
	}
}
