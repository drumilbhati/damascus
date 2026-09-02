package stress

import (
	"context"
	"fmt"
	"time"
)

// RateStep represents a single step in a stepped load plan.
type RateStep struct {
	StepNumber int           `json:"step_number"`
	Rate       int           `json:"rate"`
	Duration   time.Duration `json:"duration"`
}

// StepHandler is invoked on each step transition with the step number and current target rate.
// If the handler returns an error, execution terminates early and returns that error.
type StepHandler func(ctx context.Context, step int, rate int) error

// StepController executes a stepped load plan, gradually stepping traffic up from InitialRate to MaxRate.
type StepController struct {
	Plan         LoadPlan
	StepDuration time.Duration // Optional duration override (useful for testing). If 0, uses Plan.StepDurationSeconds.
}

// NewStepController initializes a new StepController for the given LoadPlan.
func NewStepController(plan LoadPlan) *StepController {
	return &StepController{
		Plan: plan,
	}
}

// Duration returns the active step duration.
func (c *StepController) Duration() time.Duration {
	if c.StepDuration > 0 {
		return c.StepDuration
	}
	return time.Duration(c.Plan.StepDurationSeconds) * time.Second
}

// Validate checks the load plan parameters for validity.
func (c *StepController) Validate() error {
	if c.Plan.InitialRate <= 0 {
		return fmt.Errorf("initial_rate must be greater than 0")
	}
	if c.Plan.MaxRate < c.Plan.InitialRate {
		return fmt.Errorf("max_rate (%d) cannot be less than initial_rate (%d)", c.Plan.MaxRate, c.Plan.InitialRate)
	}
	if c.Plan.StepRate < 0 {
		return fmt.Errorf("step_rate cannot be negative")
	}
	if c.Plan.StepRate == 0 && c.Plan.MaxRate > c.Plan.InitialRate {
		return fmt.Errorf("step_rate must be greater than 0 when max_rate exceeds initial_rate")
	}
	if c.StepDuration <= 0 && c.Plan.StepDurationSeconds <= 0 {
		return fmt.Errorf("step_duration_seconds must be greater than 0")
	}
	return nil
}

// CalculateSteps returns the sequence of planned rate steps without executing them.
func (c *StepController) CalculateSteps() ([]RateStep, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	duration := c.Duration()
	steps := make([]RateStep, 0)
	stepNumber := 1
	currentRate := c.Plan.InitialRate

	for {
		if currentRate > c.Plan.MaxRate {
			currentRate = c.Plan.MaxRate
		}

		steps = append(steps, RateStep{
			StepNumber: stepNumber,
			Rate:       currentRate,
			Duration:   duration,
		})

		if currentRate >= c.Plan.MaxRate {
			break
		}

		currentRate += c.Plan.StepRate
		stepNumber++
	}

	return steps, nil
}

// Run executes the stepped load plan, transitioning rates from InitialRate to MaxRate.
// Step 1: InitialRate for StepDuration
// Step 2: InitialRate + StepRate for StepDuration
// Continues stepping until MaxRate is reached and completed, or context is cancelled.
func (c *StepController) Run(ctx context.Context, handler StepHandler) error {
	if err := c.Validate(); err != nil {
		return err
	}

	duration := c.Duration()
	stepNumber := 1
	currentRate := c.Plan.InitialRate

	for {
		// Check for early cancellation before executing step
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if currentRate > c.Plan.MaxRate {
			currentRate = c.Plan.MaxRate
		}

		if handler != nil {
			if err := handler(ctx, stepNumber, currentRate); err != nil {
				return err
			}
		}

		// Wait for StepDuration or context cancellation
		timer := time.NewTimer(duration)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		if currentRate >= c.Plan.MaxRate {
			break
		}

		currentRate += c.Plan.StepRate
		stepNumber++
	}

	return nil
}

// RunStepPlan is a convenience function to run a stepped load plan with the default StepController.
func RunStepPlan(ctx context.Context, plan LoadPlan, handler StepHandler) error {
	controller := NewStepController(plan)
	return controller.Run(ctx, handler)
}
