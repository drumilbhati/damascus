package stress_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"damascus/internal/stress"
)

func TestStepController_Validate(t *testing.T) {
	tests := []struct {
		name        string
		plan        stress.LoadPlan
		overrideDur time.Duration
		wantErr     bool
	}{
		{
			name: "valid plan",
			plan: stress.LoadPlan{
				TargetURL:           "http://localhost:8080/cart",
				Method:              "POST",
				InitialRate:         100,
				StepRate:            50,
				MaxRate:             300,
				StepDurationSeconds: 10,
			},
			wantErr: false,
		},
		{
			name: "initial rate zero",
			plan: stress.LoadPlan{
				InitialRate:         0,
				StepRate:            50,
				MaxRate:             300,
				StepDurationSeconds: 10,
			},
			wantErr: true,
		},
		{
			name: "initial rate negative",
			plan: stress.LoadPlan{
				InitialRate:         -10,
				StepRate:            50,
				MaxRate:             300,
				StepDurationSeconds: 10,
			},
			wantErr: true,
		},
		{
			name: "max rate less than initial rate",
			plan: stress.LoadPlan{
				InitialRate:         200,
				StepRate:            50,
				MaxRate:             100,
				StepDurationSeconds: 10,
			},
			wantErr: true,
		},
		{
			name: "step rate negative",
			plan: stress.LoadPlan{
				InitialRate:         100,
				StepRate:            -10,
				MaxRate:             200,
				StepDurationSeconds: 10,
			},
			wantErr: true,
		},
		{
			name: "step rate zero when max rate exceeds initial rate",
			plan: stress.LoadPlan{
				InitialRate:         100,
				StepRate:            0,
				MaxRate:             200,
				StepDurationSeconds: 10,
			},
			wantErr: true,
		},
		{
			name: "step rate zero allowed when initial rate equals max rate",
			plan: stress.LoadPlan{
				InitialRate:         100,
				StepRate:            0,
				MaxRate:             100,
				StepDurationSeconds: 10,
			},
			wantErr: false,
		},
		{
			name: "step duration zero without override",
			plan: stress.LoadPlan{
				InitialRate:         100,
				StepRate:            50,
				MaxRate:             200,
				StepDurationSeconds: 0,
			},
			wantErr: true,
		},
		{
			name: "step duration zero with valid override duration",
			plan: stress.LoadPlan{
				InitialRate:         100,
				StepRate:            50,
				MaxRate:             200,
				StepDurationSeconds: 0,
			},
			overrideDur: 50 * time.Millisecond,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := stress.NewStepController(tt.plan)
			if tt.overrideDur > 0 {
				controller.StepDuration = tt.overrideDur
			}
			err := controller.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStepController_CalculateSteps(t *testing.T) {
	t.Run("exact step division", func(t *testing.T) {
		plan := stress.LoadPlan{
			InitialRate:         100,
			StepRate:            50,
			MaxRate:             250,
			StepDurationSeconds: 5,
		}
		controller := stress.NewStepController(plan)
		steps, err := controller.CalculateSteps()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedRates := []int{100, 150, 200, 250}
		if len(steps) != len(expectedRates) {
			t.Fatalf("expected %d steps, got %d", len(expectedRates), len(steps))
		}

		for i, step := range steps {
			if step.StepNumber != i+1 {
				t.Errorf("step %d: expected StepNumber %d, got %d", i, i+1, step.StepNumber)
			}
			if step.Rate != expectedRates[i] {
				t.Errorf("step %d: expected Rate %d, got %d", i, expectedRates[i], step.Rate)
			}
			if step.Duration != 5*time.Second {
				t.Errorf("step %d: expected Duration 5s, got %v", i, step.Duration)
			}
		}
	})

	t.Run("non-exact step division capped at max rate", func(t *testing.T) {
		plan := stress.LoadPlan{
			InitialRate:         100,
			StepRate:            60,
			MaxRate:             200,
			StepDurationSeconds: 2,
		}
		controller := stress.NewStepController(plan)
		steps, err := controller.CalculateSteps()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedRates := []int{100, 160, 200}
		if len(steps) != len(expectedRates) {
			t.Fatalf("expected %d steps, got %d", len(expectedRates), len(steps))
		}

		for i, step := range steps {
			if step.Rate != expectedRates[i] {
				t.Errorf("step %d: expected Rate %d, got %d", i, expectedRates[i], step.Rate)
			}
		}
	})

	t.Run("single step when initial rate equals max rate", func(t *testing.T) {
		plan := stress.LoadPlan{
			InitialRate:         150,
			StepRate:            50,
			MaxRate:             150,
			StepDurationSeconds: 3,
		}
		controller := stress.NewStepController(plan)
		steps, err := controller.CalculateSteps()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(steps) != 1 {
			t.Fatalf("expected 1 step, got %d", len(steps))
		}
		if steps[0].Rate != 150 {
			t.Errorf("expected Rate 150, got %d", steps[0].Rate)
		}
	})

	t.Run("invalid plan returns error", func(t *testing.T) {
		plan := stress.LoadPlan{
			InitialRate: -10,
		}
		controller := stress.NewStepController(plan)
		_, err := controller.CalculateSteps()
		if err == nil {
			t.Error("expected error for invalid plan, got nil")
		}
	})
}

func TestStepController_Run_Success(t *testing.T) {
	plan := stress.LoadPlan{
		InitialRate: 100,
		StepRate:    50,
		MaxRate:     200,
	}

	controller := stress.NewStepController(plan)
	controller.StepDuration = 10 * time.Millisecond // Fast duration for testing

	type stepRecord struct {
		step int
		rate int
	}

	var mu sync.Mutex
	var recorded []stepRecord

	ctx := context.Background()
	err := controller.Run(ctx, func(ctx context.Context, step int, rate int) error {
		mu.Lock()
		defer mu.Unlock()
		recorded = append(recorded, stepRecord{step: step, rate: rate})
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error during Run: %v", err)
	}

	expected := []stepRecord{
		{step: 1, rate: 100},
		{step: 2, rate: 150},
		{step: 3, rate: 200},
	}

	if len(recorded) != len(expected) {
		t.Fatalf("expected %d recorded steps, got %d", len(expected), len(recorded))
	}

	for i, exp := range expected {
		if recorded[i] != exp {
			t.Errorf("step %d mismatch: expected %+v, got %+v", i, exp, recorded[i])
		}
	}
}

func TestStepController_Run_ContextCancellation(t *testing.T) {
	plan := stress.LoadPlan{
		InitialRate: 100,
		StepRate:    50,
		MaxRate:     500,
	}

	controller := stress.NewStepController(plan)
	controller.StepDuration = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())

	stepCount := 0
	err := controller.Run(ctx, func(ctx context.Context, step int, rate int) error {
		stepCount++
		if step == 2 {
			// Cancel context during step 2
			cancel()
		}
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	if stepCount > 2 {
		t.Errorf("expected at most 2 steps executed before cancellation, got %d", stepCount)
	}
}

func TestStepController_Run_AlreadyCancelledContext(t *testing.T) {
	plan := stress.LoadPlan{
		InitialRate: 100,
		StepRate:    50,
		MaxRate:     200,
	}

	controller := stress.NewStepController(plan)
	controller.StepDuration = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := controller.Run(ctx, func(ctx context.Context, step int, rate int) error {
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestStepController_Run_HandlerError(t *testing.T) {
	plan := stress.LoadPlan{
		InitialRate: 100,
		StepRate:    50,
		MaxRate:     300,
	}

	controller := stress.NewStepController(plan)
	controller.StepDuration = 10 * time.Millisecond

	expectedErr := errors.New("simulated handler failure")

	err := controller.Run(context.Background(), func(ctx context.Context, step int, rate int) error {
		if step == 2 {
			return expectedErr
		}
		return nil
	})

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestRunStepPlan(t *testing.T) {
	plan := stress.LoadPlan{
		InitialRate:         100,
		StepRate:            50,
		MaxRate:             100,
		StepDurationSeconds: 1,
	}

	called := false
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := stress.RunStepPlan(ctx, plan, func(ctx context.Context, step int, rate int) error {
		called = true
		if rate != 100 {
			t.Errorf("expected rate 100, got %d", rate)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected handler to be called")
	}
}

func TestLoadPlan_Validate(t *testing.T) {
	validPlan := stress.LoadPlan{
		InitialRate:         50,
		StepRate:            25,
		MaxRate:             200,
		StepDurationSeconds: 10,
	}
	if err := validPlan.Validate(); err != nil {
		t.Errorf("expected valid plan to pass, got: %v", err)
	}

	invalidPlan := stress.LoadPlan{
		InitialRate: 0,
	}
	if err := invalidPlan.Validate(); err == nil {
		t.Error("expected invalid plan to fail, got nil")
	}
}
