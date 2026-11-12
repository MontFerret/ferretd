package client

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

type (
	// RuntimeDescriptor describes a configured target without connection settings
	// or claims about reachability, execution, or debugger support.
	RuntimeDescriptor struct {
		Name     string
		Label    string
		Provider string
	}

	// RuntimeCatalog is an ordered configuration snapshot with one default name.
	RuntimeCatalog struct {
		DefaultRuntime string
		Runtimes       []RuntimeDescriptor
	}
)

// ListRuntimes discovers configured targets without connecting to them. Older
// compatible daemons may return ErrUnsupportedOperation.
func (c *Client) ListRuntimes(ctx context.Context) (RuntimeCatalog, error) {
	if err := ctx.Err(); err != nil {
		return RuntimeCatalog{}, err
	}

	response, err := c.daemon.ListRuntimes(ctx, &daemonv1.ListRuntimesRequest{})
	if err != nil {
		mapped := mapError(ctx, err)
		if status.Code(mapped) == codes.Unimplemented {
			return RuntimeCatalog{}, errors.Join(ErrUnsupportedOperation, mapped)
		}

		return RuntimeCatalog{}, mapped
	}

	return fromProtoRuntimeCatalog(response)
}
