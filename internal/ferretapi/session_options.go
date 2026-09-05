package ferretapi

import (
	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2"
)

type sessionOptions struct {
	options []ferret.SessionOption
}

func newSessionOptions(options []api.SessionOption) (*sessionOptions, error) {
	result := &sessionOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (o *sessionOptions) SetParam(name string, value any) error {
	o.options = append(o.options, ferret.WithSessionParam(name, value))

	return nil
}

func (o *sessionOptions) SetParams(parameters map[string]any) error {
	o.options = append(o.options, ferret.WithSessionParams(parameters))

	return nil
}

func (o *sessionOptions) SetOutputContentType(contentType string) error {
	o.options = append(o.options, ferret.WithOutputContentType(contentType))

	return nil
}

func (o *sessionOptions) SetFSRoot(root string) error {
	o.options = append(o.options, ferret.WithSessionFSRoot(root))

	return nil
}
