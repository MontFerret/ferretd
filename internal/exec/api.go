package exec

import (
	"io"
	"reflect"

	"github.com/MontFerret/api"
)

func isNilAPI(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func closeAPIPlan(plan api.Plan) error {
	return closeAPIResource(plan)
}

func closeAPIResource(resource io.Closer) error {
	if isNilAPI(resource) {
		return nil
	}

	return resource.Close()
}
