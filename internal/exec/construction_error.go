package exec

import (
	"context"
	"errors"
)

// constructionError preserves both the operation's context identity and a
// parent-close or caller-provided cancellation cause.
func constructionError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(err, context.Cause(ctx))
	}

	return nil
}
