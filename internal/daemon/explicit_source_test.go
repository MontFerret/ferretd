package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	supportedclient "github.com/MontFerret/ferretd/client"
	"github.com/MontFerret/ferretd/internal/source"
)

func TestSupportedClientExecutesExplicitExcludedSource(t *testing.T) {
	for _, name := range []string{"directory", "symlink root"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()

			if name == "symlink root" {
				alias := filepath.Join(t.TempDir(), "workspace")
				if err := os.Symlink(root, alias); err != nil {
					t.Skipf("create workspace root symlink: %v", err)
				}

				root = alias
			}

			assertExplicitSourceExecution(t, root)
		})
	}
}

func assertExplicitSourceExecution(t *testing.T, root string) {
	t.Helper()

	program := filepath.Join(root, ".tmp", "test.fql")
	if err := os.Mkdir(filepath.Dir(program), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(program, []byte("RETURN 41"), 0o600); err != nil {
		t.Fatal(err)
	}

	endpoint := testEndpoint(t)

	d, err := New(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan error, 1)
	go func() { started <- d.Start(context.Background()) }()
	waitForEndpoint(t, endpoint)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := d.Stop(ctx); err != nil {
			t.Error(err)
		}

		if err := <-started; err != nil {
			t.Error(err)
		}
	})

	publicEndpoint, err := supportedclient.ParseEndpoint(endpoint.String())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := supportedclient.Dial(ctx, supportedclient.WithEndpoint(publicEndpoint))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = client.Close() })

	opened, err := client.Workspaces().Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	first, err := client.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: ".tmp/test.fql"})
	if err != nil {
		t.Fatalf("explicit CreateSession: %v", err)
	}

	// Source identity preserves the selected workspace root's spelling, including
	// a root symlink or macOS's /var alias for /private/var.
	uri, err := source.URIFromPath(program)
	if err != nil {
		t.Fatal(err)
	}

	if first.Source.RelativePath != ".tmp/test.fql" || first.Source.URI != string(uri) || first.Source.WorkspaceID != opened.ID {
		t.Fatalf("source identity = %+v", first.Source)
	}

	if err := os.WriteFile(program, []byte("RETURN 42"), 0o600); err != nil {
		t.Fatal(err)
	}

	second, err := client.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: ".tmp/test.fql"})
	if err != nil {
		t.Fatal(err)
	}

	if second.Source.Revision <= first.Source.Revision {
		t.Fatal("new session did not refresh source revision")
	}

	for _, test := range []struct {
		session supportedclient.SessionID
		want    string
	}{{first.ID, "41"}, {second.ID, "42"}} {
		created, err := client.Executions().CreateExecution(ctx, supportedclient.CreateExecutionRequest{SessionID: test.session})
		if err != nil {
			t.Fatal(err)
		}

		watch, err := client.Executions().WatchExecution(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := client.Executions().RunExecution(ctx, created.ID); err != nil {
			t.Fatal(err)
		}

		for {
			event, err := watch.Recv()
			if err != nil {
				t.Fatal(err)
			}

			if !event.Execution.State.Terminal() {
				continue
			}

			if event.Kind != supportedclient.ExecutionEventCompleted || event.Execution.Output == nil || string(event.Execution.Output.Data) != test.want {
				t.Fatalf("execution did not use immutable selected source: %+v", event)
			}

			break
		}
	}

	if err := os.Remove(program); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: ".tmp/test.fql"}); !errors.Is(err, supportedclient.ErrExecutionSourceNotFound) {
		t.Fatalf("missing explicit source classification: %v", err)
	}
}
