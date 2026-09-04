package exec

import "github.com/MontFerret/api"

type (
	// FailureCategory classifies a failed execution phase.
	FailureCategory uint8

	// Failure adds the ordinary execution phase to shared runtime failure details.
	Failure struct {
		Category FailureCategory
		RuntimeFailure
	}

	// ExecutionSnapshot is an immutable view of one daemon Execution.
	ExecutionSnapshot struct {
		ID         ExecutionID
		Session    SessionID
		State      State
		Parameters Parameters
		Options    RuntimeOptions
		Output     *api.Output
		Failure    *Failure
	}
)

const (
	// FailureSessionCreation identifies Plan.NewSession failure.
	FailureSessionCreation FailureCategory = iota + 1
	// FailureRuntime identifies Universal Session.Run failure.
	FailureRuntime
	// FailureCleanup identifies Universal Session cleanup failure.
	FailureCleanup
)

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows Parameters' recursive container contract.
func (s ExecutionSnapshot) Clone() ExecutionSnapshot {
	result := s
	result.Parameters = s.Parameters.Clone()
	result.Output = cloneOutput(s.Output)
	result.Failure = s.Failure.Clone()

	return result
}

// Clone returns an independent copy of the categorized execution failure.
func (f *Failure) Clone() *Failure {
	if f == nil {
		return nil
	}

	details := f.RuntimeFailure.Clone()

	return &Failure{Category: f.Category, RuntimeFailure: *details}
}
