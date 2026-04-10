package botruntime

import "time"

type RetryPolicy struct {
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	MaxAttempts int
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		BaseBackoff: 15 * time.Second,
		MaxBackoff:  2 * time.Minute,
		MaxAttempts: 5,
	}
}

func (p RetryPolicy) NextDelay(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}

	delay := p.BaseBackoff
	for step := 1; step < attempt; step++ {
		delay *= 2
		if delay >= p.MaxBackoff {
			return p.MaxBackoff
		}
	}

	if delay > p.MaxBackoff {
		return p.MaxBackoff
	}

	return delay
}
