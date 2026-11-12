package client

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

func TestListRuntimesConversion(t *testing.T) {
	original := &daemonv1.ListRuntimesResponse{DefaultRuntime: "app", Runtimes: []*daemonv1.RuntimeDescriptor{{Name: "app", Label: "Application", Provider: "future-provider"}}}
	c := &Client{daemon: &runtimeClientStub{response: original}}

	result, err := c.ListRuntimes(t.Context())
	if err != nil || result.DefaultRuntime != "app" || len(result.Runtimes) != 1 || result.Runtimes[0].Provider != "future-provider" {
		t.Fatalf("ListRuntimes = %+v, %v", result, err)
	}

	result.Runtimes[0].Name = "changed"
	if original.Runtimes[0].Name != "app" {
		t.Fatal("conversion aliases response")
	}
}

func TestListRuntimesRejectsIncompleteResponses(t *testing.T) {
	valid := &daemonv1.RuntimeDescriptor{Name: "local", Label: "Built-in Ferret", Provider: "native"}
	for _, test := range []struct {
		name     string
		response *daemonv1.ListRuntimesResponse
	}{
		{"nil", nil}, {"empty", &daemonv1.ListRuntimesResponse{}},
		{"missing default", &daemonv1.ListRuntimesResponse{Runtimes: []*daemonv1.RuntimeDescriptor{valid}}},
		{"empty list", &daemonv1.ListRuntimesResponse{DefaultRuntime: "local"}},
		{"nil descriptor", &daemonv1.ListRuntimesResponse{DefaultRuntime: "local", Runtimes: []*daemonv1.RuntimeDescriptor{nil}}},
		{"missing name", &daemonv1.ListRuntimesResponse{DefaultRuntime: "local", Runtimes: []*daemonv1.RuntimeDescriptor{{Label: "Local", Provider: "native"}}}},
		{"missing label", &daemonv1.ListRuntimesResponse{DefaultRuntime: "local", Runtimes: []*daemonv1.RuntimeDescriptor{{Name: "local", Provider: "native"}}}},
		{"missing provider", &daemonv1.ListRuntimesResponse{DefaultRuntime: "local", Runtimes: []*daemonv1.RuntimeDescriptor{{Name: "local", Label: "Local"}}}},
		{"duplicate", &daemonv1.ListRuntimesResponse{DefaultRuntime: "local", Runtimes: []*daemonv1.RuntimeDescriptor{valid, valid}}},
		{"absent default", &daemonv1.ListRuntimesResponse{DefaultRuntime: "missing", Runtimes: []*daemonv1.RuntimeDescriptor{valid}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := &Client{daemon: &runtimeClientStub{response: test.response}}

			_, err := c.ListRuntimes(t.Context())
			if !errors.Is(err, errIncompleteRuntimeCatalog) {
				t.Fatalf("ListRuntimes = %v", err)
			}
		})
	}
}

func TestListRuntimesErrors(t *testing.T) {
	original := status.Error(codes.Unimplemented, "unknown method ListRuntimes")
	stub := &runtimeClientStub{err: original}
	c := &Client{daemon: stub}

	_, err := c.ListRuntimes(t.Context())
	if !errors.Is(err, ErrUnsupportedOperation) || !errors.Is(err, original) || status.Code(err) != codes.Unimplemented {
		t.Fatalf("unsupported = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = c.ListRuntimes(ctx)
	if !errors.Is(err, context.Canceled) || stub.calls != 1 {
		t.Fatalf("canceled = %v; calls = %d", err, stub.calls)
	}

	for _, test := range []struct {
		code codes.Code
		want error
	}{
		{codes.Canceled, context.Canceled}, {codes.DeadlineExceeded, context.DeadlineExceeded}, {codes.Unavailable, ErrDaemonUnavailable},
	} {
		stub.err = status.Error(test.code, "failed")
		if _, err := c.ListRuntimes(t.Context()); !errors.Is(err, test.want) {
			t.Fatalf("code %v = %v", test.code, err)
		}
	}
}
