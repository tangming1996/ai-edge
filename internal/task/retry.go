package task

import (
	"math"
	"time"
)

const (
	DefaultMaxRetries   = 3
	baseBackoffInterval = 2 * time.Second
	maxBackoffInterval  = 60 * time.Second
)

// RetryPolicy encapsulates retry parameters for a task.
type RetryPolicy struct {
	MaxRetries     int
	RetryCount     int
	TimeoutSeconds int
}

// CanRetry returns true if the task has remaining retry budget.
func (p RetryPolicy) CanRetry() bool {
	return p.RetryCount < p.MaxRetries
}

// NextBackoff returns the exponential backoff duration for the current attempt.
// Formula: min(base * 2^retryCount, maxBackoff)
func (p RetryPolicy) NextBackoff() time.Duration {
	exp := math.Pow(2, float64(p.RetryCount))
	d := time.Duration(float64(baseBackoffInterval) * exp)
	if d > maxBackoffInterval {
		d = maxBackoffInterval
	}
	return d
}

// RecoverableError represents an error that allows retry.
type RecoverableError struct {
	Err error
}

func (e *RecoverableError) Error() string {
	return "recoverable: " + e.Err.Error()
}

func (e *RecoverableError) Unwrap() error { return e.Err }

// UnrecoverableError represents a permanent failure that should not be retried.
type UnrecoverableError struct {
	Err error
}

func (e *UnrecoverableError) Error() string {
	return "unrecoverable: " + e.Err.Error()
}

func (e *UnrecoverableError) Unwrap() error { return e.Err }

// IsRecoverable returns true if the error is a RecoverableError.
func IsRecoverable(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*RecoverableError)
	return ok
}

// ClassifyResultStatus returns the status text a task should transition to
// based on the reported error and retry budget.
// "success" → "Success"; recoverable + budget left → "Retrying"; else → "Failed".
func ClassifyResultStatus(errMsg string, policy RetryPolicy) string {
	if errMsg == "" {
		return "Success"
	}
	if policy.CanRetry() {
		return "Retrying"
	}
	return "Failed"
}

// TimeoutExceeded checks whether the elapsed duration exceeds the task timeout.
func TimeoutExceeded(started time.Time, timeoutSeconds int) bool {
	if timeoutSeconds <= 0 {
		return false
	}
	return time.Since(started) > time.Duration(timeoutSeconds)*time.Second
}
