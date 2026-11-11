package dap

import "errors"

var (
	errAdapterClosing     = errors.New("DAP adapter closing")
	errRequestOverload    = errors.New("DAP request queue overloaded")
	errLaunchCancelled    = errors.New("cancelled")
	errLaunchTerminated   = errors.New("launch terminated before configurationDone")
	errLaunchDisconnected = errors.New("launch disconnected before configurationDone")

	errInvalidSourcePosition = errors.New("invalid source position")
	errNilInput              = errors.New("dap: nil input")
	errNilOutput             = errors.New("dap: nil output")
)
