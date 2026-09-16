package jupiter

import (
	"context"
	"fmt"
	"time"
)

// PollConfig controls Jupiter execute polling.
type PollConfig struct {
	MaxAttempts int
	Interval    time.Duration
}

// DefaultPollConfig is the standard poll loop for Jupiter execute.
// Buy/sell paths use this; unit tests inject a short PollConfig instead.
func DefaultPollConfig() PollConfig {
	return PollConfig{
		MaxAttempts: 30,
		Interval:    2 * time.Second,
	}
}

// PollUntilConfirmed polls Jupiter /execute until Success with code 0 or terminal failure.
func PollUntilConfirmed(ctx context.Context, client Client, params PollExecuteParams, cfg PollConfig) (ExecuteResult, error) {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Millisecond
	}

	var last ExecuteResult
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(cfg.Interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ExecuteResult{}, ctx.Err()
			case <-timer.C:
			}
		}

		result, err := client.PollExecute(ctx, params)
		if err != nil {
			return ExecuteResult{}, err
		}
		result.RequestID = params.RequestID
		last = result

		if attempt > 0 {
			logPollTransition(params.GroupID, params.UserID, params.Symbol, result.Signature, last.Status, result.Status, result.Code)
		}

		if result.IsConfirmedSuccess() {
			logPollTransition(params.GroupID, params.UserID, params.Symbol, result.Signature, ExecuteStatusPending, result.Status, result.Code)
			return result, nil
		}
		if result.IsTerminal() && !result.IsConfirmedSuccess() {
			logPollTransition(params.GroupID, params.UserID, params.Symbol, result.Signature, ExecuteStatusPending, result.Status, result.Code)
			return result, fmt.Errorf("jupiter: execute terminal failure status=%s code=%d: %s", result.Status, result.Code, result.Error)
		}
	}

	if last.IsConfirmedSuccess() {
		return last, nil
	}
	return last, fmt.Errorf("jupiter: execute poll exhausted attempts status=%s code=%d", last.Status, last.Code)
}
