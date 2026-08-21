package dap

import "strings"

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") {
		return value
	}

	return value + "\n"
}
