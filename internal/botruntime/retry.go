package botruntime

import (
	"errors"
	"math/rand"
	"strings"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
)

type RetryPolicy struct {
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	MaxAttempts int
	JitterFrac  float64 // Jitter as fraction (e.g., 0.15 = ±15%)
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		BaseBackoff: 15 * time.Second,
		MaxBackoff:  2 * time.Minute,
		MaxAttempts: 5,
		JitterFrac:  0.15, // ±15% jitter to prevent thundering herd
	}
}

// NextDelay calculates exponential backoff with jitter.
// Delay = min(BaseBackoff * 2^(attempt-1), MaxBackoff) * (1 ± JitterFrac)
func (p RetryPolicy) NextDelay(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}

	delay := p.BaseBackoff
	for step := 1; step < attempt; step++ {
		delay *= 2
		if delay >= p.MaxBackoff {
			delay = p.MaxBackoff
			break
		}
	}

	if delay > p.MaxBackoff {
		delay = p.MaxBackoff
	}

	// Apply jitter: add random variance within ±JitterFrac
	if p.JitterFrac > 0 {
		jitterRange := float64(delay) * p.JitterFrac
		jitterDelta := (rand.Float64() - 0.5) * 2 * jitterRange // Random in [-jitterRange, jitterRange]
		delay = time.Duration(float64(delay) + jitterDelta)
		// Ensure delay never goes negative or zero
		if delay <= 0 {
			delay = p.BaseBackoff / 2
		}
	}

	return delay
}

// IsExhausted returns true if retry count has reached max attempts.
func (p RetryPolicy) IsExhausted(retryCount int) bool {
	return p.MaxAttempts > 0 && retryCount >= p.MaxAttempts
}

// ErrorType categorizes errors for retry decision-making.
type ErrorType int

const (
	ErrorTypeTransient ErrorType = iota // Retryable (e.g., timeout, rate limit)
	ErrorTypeTerminal                     // Non-retryable (e.g., insufficient balance, invalid account)
	ErrorTypeUnknown                      // Treat as terminal by default (fail closed)
)

// ClassifyTransferError categorizes payment transfer errors.
func ClassifyTransferError(err error) ErrorType {
	if err == nil {
		return ErrorTypeTerminal
	}

	// Terminal errors: never retry
	if errors.Is(err, service.ErrInsufficientBalance) {
		return ErrorTypeTerminal
	}

	// Transient indicators: retry-worthy
	message := strings.ToLower(err.Error())
	transientMarkers := []string{
		"timeout", "tempor", "connection reset", "eof",
		"unavailable", "rate limit", "429", "500", "502", "503", "504",
		"try again", "temporarily", "transient",
	}
	for _, marker := range transientMarkers {
		if strings.Contains(message, marker) {
			return ErrorTypeTransient
		}
	}

	// Unknown errors default to terminal (fail closed)
	return ErrorTypeTerminal
}
