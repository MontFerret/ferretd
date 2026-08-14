package language

import (
	"github.com/MontFerret/ferret/v2/pkg/runtime"

	"github.com/MontFerret/ferretd/internal/workspace"
)

// Options configures a protocol-neutral language service.
type Options struct {
	Workspaces *workspace.Manager
	Functions  *runtime.Functions
	Params     runtime.Params
}
