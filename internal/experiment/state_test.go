package experiment_test

import (
	"errors"
	"testing"

	"damascus/internal/experiment"
)

// TestValidateTransition_ValidTransitions asserts that all nominal progressive transitions
// and non-terminal abort transitions succeed without error, and that CanTransition returns true.
func TestValidateTransition_ValidTransitions(t *testing.T) {
	validCases := []struct {
		current experiment.ExperimentState
		target  experiment.ExperimentState
	}{
		// Nominal progressive flow
		{experiment.StateCreated, experiment.StatePlanned},
		{experiment.StatePlanned, experiment.StateRunning},
		{experiment.StateRunning, experiment.StateStopping},
		{experiment.StateStopping, experiment.StateAnalyzing},
		{experiment.StateAnalyzing, experiment.StateReporting},
		{experiment.StateReporting, experiment.StateCompleted},

		// Aborts from all active non-terminal states
		{experiment.StateCreated, experiment.StateAborted},
		{experiment.StatePlanned, experiment.StateAborted},
		{experiment.StateRunning, experiment.StateAborted},
		{experiment.StateStopping, experiment.StateAborted},
		{experiment.StateAnalyzing, experiment.StateAborted},
		{experiment.StateReporting, experiment.StateAborted},
	}

	for _, tc := range validCases {
		t.Run(string(tc.current)+"->"+string(tc.target), func(t *testing.T) {
			err := experiment.ValidateTransition(tc.current, tc.target)
			if err != nil {
				t.Fatalf("expected valid transition from %s to %s, got error: %v", tc.current, tc.target, err)
			}
			if !experiment.CanTransition(tc.current, tc.target) {
				t.Fatalf("CanTransition(%s, %s) returned false, expected true", tc.current, tc.target)
			}
		})
	}
}

// TestValidateTransition_InvalidTransitions performs a full Cartesian sweep across all 8 states
// (64 possible combinations) to verify that all 52 disallowed transitions return ErrInvalidTransition.
func TestValidateTransition_InvalidTransitions(t *testing.T) {
	allStates := []experiment.ExperimentState{
		experiment.StateCreated,
		experiment.StatePlanned,
		experiment.StateRunning,
		experiment.StateStopping,
		experiment.StateAnalyzing,
		experiment.StateReporting,
		experiment.StateCompleted,
		experiment.StateAborted,
	}

	validMap := map[experiment.ExperimentState]map[experiment.ExperimentState]bool{
		experiment.StateCreated: {
			experiment.StatePlanned: true,
			experiment.StateAborted: true,
		},
		experiment.StatePlanned: {
			experiment.StateRunning: true,
			experiment.StateAborted: true,
		},
		experiment.StateRunning: {
			experiment.StateStopping: true,
			experiment.StateAborted:  true,
		},
		experiment.StateStopping: {
			experiment.StateAnalyzing: true,
			experiment.StateAborted:   true,
		},
		experiment.StateAnalyzing: {
			experiment.StateReporting: true,
			experiment.StateAborted:   true,
		},
		experiment.StateReporting: {
			experiment.StateCompleted: true,
			experiment.StateAborted:   true,
		},
		experiment.StateCompleted: {},
		experiment.StateAborted:   {},
	}

	for _, from := range allStates {
		for _, to := range allStates {
			if validMap[from][to] {
				continue
			}

			t.Run("Invalid_"+string(from)+"->"+string(to), func(t *testing.T) {
				err := experiment.ValidateTransition(from, to)
				if err == nil {
					t.Fatalf("expected error transitioning from %s to %s, got nil", from, to)
				}
				if !errors.Is(err, experiment.ErrInvalidTransition) {
					t.Fatalf("expected error wrapping ErrInvalidTransition, got %v", err)
				}
				if experiment.CanTransition(from, to) {
					t.Fatalf("CanTransition(%s, %s) returned true, expected false", from, to)
				}
			})
		}
	}
}

// TestValidateTransition_UnknownState verifies that unrecognized or arbitrary state strings
// are rejected with ErrInvalidTransition.
func TestValidateTransition_UnknownState(t *testing.T) {
	unknown := experiment.ExperimentState("UNKNOWN_STATE")

	err := experiment.ValidateTransition(unknown, experiment.StatePlanned)
	if err == nil {
		t.Fatalf("expected error for unknown current state, got nil")
	}
	if !errors.Is(err, experiment.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for unknown state, got %v", err)
	}

	if experiment.CanTransition(unknown, experiment.StatePlanned) {
		t.Fatalf("CanTransition should return false for unknown state")
	}
}

// TestValidateTransition_TerminalStatesCannotTransition verifies that terminal states
// (COMPLETED and ABORTED) cannot transition into any state, including themselves.
func TestValidateTransition_TerminalStatesCannotTransition(t *testing.T) {
	terminals := []experiment.ExperimentState{
		experiment.StateCompleted,
		experiment.StateAborted,
	}

	allStates := []experiment.ExperimentState{
		experiment.StateCreated,
		experiment.StatePlanned,
		experiment.StateRunning,
		experiment.StateStopping,
		experiment.StateAnalyzing,
		experiment.StateReporting,
		experiment.StateCompleted,
		experiment.StateAborted,
	}

	for _, term := range terminals {
		for _, target := range allStates {
			t.Run(string(term)+"->"+string(target), func(t *testing.T) {
				err := experiment.ValidateTransition(term, target)
				if err == nil {
					t.Fatalf("expected error transitioning out of terminal state %s to %s, got nil", term, target)
				}
				if experiment.CanTransition(term, target) {
					t.Fatalf("expected CanTransition to be false from terminal state %s to %s", term, target)
				}
			})
		}
	}
}
