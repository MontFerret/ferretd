package lsp

import "errors"

var (
	errNilLanguageService        = errors.New("lsp: nil language service")
	errIncrementalTextChanges    = errors.New("incremental text document changes are not supported")
	errUnsupportedDocumentChange = errors.New("unsupported text document change")
)
