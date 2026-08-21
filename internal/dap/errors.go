package dap

import "errors"

var (
	errNilInput  = errors.New("dap: nil input")
	errNilOutput = errors.New("dap: nil output")
)
