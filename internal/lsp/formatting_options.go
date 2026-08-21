package lsp

import (
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
)

func formattingTabSize(options protocol.FormattingOptions) uint32 {
	const maxUint32 = uint64(^uint32(0))

	value, ok := options[protocol.FormattingOptionTabSize]
	if !ok {
		return language.DefaultTabSize
	}

	switch typed := value.(type) {
	case protocol.Integer:
		if typed > 0 {
			return uint32(typed)
		}
	case float64:
		if typed > 0 && typed <= float64(maxUint32) {
			return uint32(typed)
		}
	case int:
		if typed > 0 && uint64(typed) <= maxUint32 {
			return uint32(typed)
		}
	}

	return language.DefaultTabSize
}
