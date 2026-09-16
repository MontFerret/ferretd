package dap

import "errors"

var (
	errInvalidSourcePosition = errors.New("invalid source position")
	errNilInput              = errors.New("dap: nil input")
	errNilOutput             = errors.New("dap: nil output")
)
