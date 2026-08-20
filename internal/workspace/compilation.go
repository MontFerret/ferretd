package workspace

import "github.com/MontFerret/ferret/v2"

// Compilation owns a Ferret Plan and the source snapshot that produced it.
// A caller that transfers the Plan to a longer-lived owner must not close the
// Compilation afterward.
type Compilation struct {
	Plan   *ferret.Plan
	Source SourceSnapshot
}

// Close releases the owned Plan. Repeated calls are safe.
func (c *Compilation) Close() error {
	plan := c.Plan
	c.Plan = nil
	if plan == nil {
		return nil
	}

	return plan.Close()
}
