package exec

import "context"

func waitForDone(ctx context.Context, done <-chan struct{}, result func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return result()
	}
}
