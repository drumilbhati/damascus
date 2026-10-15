package experiment

import (
	"errors"
	"fmt"
)

// ErrInvalidTransition is returned when an experiment lifecycle transition is illegal.
var ErrInvalidTransition = errors.New("invalid state transition")

// validTransitions defines the allowed transition matrix for DAMASCUS's 8-state machine.
//
// Nominal progressive flow:
//   CREATED -> PLANNED -> RUNNING -> STOPPING -> ANALYZING -> REPORTING -> COMPLETED
//
// In addition, any non-terminal active state can transition directly to ABORTED
// if an SLA threshold is breached, a network error occurs, or the experiment is cancelled.
// Terminal states (COMPLETED, ABORTED) have no outgoing transitions.
var validTransitions = map[ExperimentState]map[ExperimentState]bool{
	StateCreated: {
		StatePlanned: true,
		StateAborted: true,
	},
	StatePlanned: {
		StateRunning: true,
		StateAborted: true,
	},
	StateRunning: {
		StateStopping: true,
		StateAborted:  true,
	},
	StateStopping: {
		StateAnalyzing: true,
		StateAborted:   true,
	},
	StateAnalyzing: {
		StateReporting: true,
		StateAborted:   true,
	},
	StateReporting: {
		StateCompleted: true,
		StateAborted:   true,
	},
	// Terminal states: empty map ensures no further transitions are permitted.
	StateCompleted: {},
	StateAborted:   {},
}

// ValidateTransition checks whether moving from 'current' to 'target' is a legal state transition.
// Returns nil if the transition is allowed; otherwise returns an error wrapping ErrInvalidTransition.
func ValidateTransition(current, target ExperimentState) error {
	allowedTargets, ok := validTransitions[current]
	if !ok {
		return fmt.Errorf("%w: unknown current state %q", ErrInvalidTransition, current)
	}

	if !allowedTargets[target] {
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidTransition, current, target)
	}

	return nil
}

// CanTransition is a convenience helper that returns true if moving from 'current' to 'target' is allowed.
func CanTransition(current, target ExperimentState) bool {
	return ValidateTransition(current, target) == nil
}
