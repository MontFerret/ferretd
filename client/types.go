// Package client provides the supported Go client for a local ferretd daemon.
package client

type (
	// APIVersion identifies a compatible daemon API generation.
	APIVersion struct {
		Major uint32
		Minor uint32
	}

	// ServerInfo describes a running daemon instance.
	ServerInfo struct {
		Version    string
		InstanceID string
		APIVersion APIVersion
	}

	// Workspace is a daemon-owned workspace snapshot.
	Workspace struct {
		ID   string
		Root string
	}
)
