package dap

import protocol "github.com/google/go-dap"

type (
	initializeResponse struct {
		protocol.Response
		Body initializeCapabilities `json:"body"`
	}

	initializeCapabilities struct {
		protocol.Capabilities
		SupportsFerretRuntimeSelection bool `json:"supportsFerretRuntimeSelection"`
	}

	launchResponse struct {
		protocol.LaunchResponse
		Body launchResponseBody `json:"body"`
	}

	launchResponseBody struct {
		RuntimeName string `json:"runtimeName"`
	}
)
