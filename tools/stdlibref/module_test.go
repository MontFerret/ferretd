package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolveFerretVersion(t *testing.T) {
	tests := []struct {
		name string
		data string
		err  error
		want string
	}{
		{
			name: "exact prerelease",
			data: `{"Path":"github.com/MontFerret/ferret/v2","Version":"v2.0.0-alpha.49"}`,
			want: "2.0.0-alpha.49",
		},
		{
			name: "replacement",
			data: `{"Path":"github.com/MontFerret/ferret/v2","Version":"v2.0.0","Replace":{"Path":"../ferret"}}`,
		},
		{
			name: "missing version",
			data: `{"Path":"github.com/MontFerret/ferret/v2"}`,
		},
		{
			name: "noncanonical version",
			data: `{"Path":"github.com/MontFerret/ferret/v2","Version":"v2.0"}`,
		},
		{
			name: "unexpected module",
			data: `{"Path":"example.com/ferret/v2","Version":"v2.0.0"}`,
		},
		{
			name: "malformed metadata",
			data: `{`,
		},
		{
			name: "command error",
			err:  errors.New("go list failed"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveFerretVersion(context.Background(), func(context.Context, ...string) ([]byte, error) {
				return []byte(test.data), test.err
			})

			if test.want != "" {
				if err != nil || got != test.want {
					t.Fatalf("resolveFerretVersion() = %q, %v, want %q", got, err, test.want)
				}

				return
			}

			if err == nil {
				t.Fatalf("resolveFerretVersion() = %q, nil", got)
			}
		})
	}
}

func TestResolveFerretVersionInvokesReadonlyGoList(t *testing.T) {
	var arguments []string
	_, err := resolveFerretVersion(context.Background(), func(_ context.Context, values ...string) ([]byte, error) {
		arguments = append(arguments, values...)

		return []byte(`{"Path":"github.com/MontFerret/ferret/v2","Version":"v2.0.0"}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(arguments, " "); got != "list -mod=readonly -m -json github.com/MontFerret/ferret/v2" {
		t.Fatalf("go arguments = %q", got)
	}
}
