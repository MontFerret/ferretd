//go:build windows

package workspace

import "strings"

func rootKey(root string) string {
	return strings.ToLower(root)
}
