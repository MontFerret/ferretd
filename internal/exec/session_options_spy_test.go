package exec

import "github.com/MontFerret/api"

type sessionOptionsSpy struct {
	params      map[string]any
	contentType string
	fsRoot      string
	fsRootCalls int
}

func newSessionOptionsSpy(options []api.SessionOption) (sessionOptionsSpy, error) {
	result := sessionOptionsSpy{params: make(map[string]any)}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(&result); err != nil {
			return sessionOptionsSpy{}, err
		}
	}

	return result, nil
}

func (o *sessionOptionsSpy) SetParam(name string, value any) error {
	o.params[name] = value

	return nil
}

func (o *sessionOptionsSpy) SetParams(parameters map[string]any) error {
	for name, value := range parameters {
		if err := o.SetParam(name, value); err != nil {
			return err
		}
	}

	return nil
}

func (o *sessionOptionsSpy) SetOutputContentType(contentType string) error {
	o.contentType = contentType

	return nil
}

func (o *sessionOptionsSpy) SetFSRoot(root string) error {
	o.fsRoot = root
	o.fsRootCalls++

	return nil
}
