package language

import (
	"fmt"
	"strings"

	"github.com/MontFerret/specs/pkg/api"
)

func renderAPIType(value *api.Type) string {
	if value == nil {
		return ""
	}

	switch value.Kind {
	case api.TypeKindNamed:
		return value.Name
	case api.TypeKindUnion:
		members := make([]string, 0, len(value.Types))
		for index := range value.Types {
			members = append(members, renderAPIType(&value.Types[index]))
		}

		return strings.Join(members, " | ")
	case api.TypeKindList:
		return "[" + renderAPIType(value.Element) + "]"
	default:
		panic(fmt.Sprintf("language: unsupported validated API type kind %q", value.Kind))
	}
}
