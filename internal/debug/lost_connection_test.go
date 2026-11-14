package debug

import "errors"

type lostConnection chan struct{}

func (c lostConnection) ConnectionDone() <-chan struct{} { return c }
func (c lostConnection) ConnectionError() error {
	select {
	case <-c:
		return errors.New("application disconnected")
	default:
		return nil
	}
}
