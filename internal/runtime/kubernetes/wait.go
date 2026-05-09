package kubernetes

import (
	"context"
	"time"
)

const (
	podPollInitial         = 500 * time.Millisecond
	podPollMax             = 3 * time.Second
	statefulSetPollInitial = 1 * time.Second
	statefulSetPollMax     = 5 * time.Second
	waitBackoffFactor      = 1.25
)

type cappedBackoff struct {
	current time.Duration
	max     time.Duration
	factor  float64
}

func newCappedBackoff(initial time.Duration, max time.Duration) *cappedBackoff {
	return &cappedBackoff{
		current: initial,
		max:     max,
		factor:  waitBackoffFactor,
	}
}

func (b *cappedBackoff) Next() time.Duration {
	delay := b.current
	next := time.Duration(float64(b.current) * b.factor)
	if next <= b.current {
		next = b.current + time.Millisecond
	}
	if next > b.max {
		next = b.max
	}
	b.current = next
	return delay
}

func jobPollInterval(elapsed time.Duration) time.Duration {
	switch {
	case elapsed < 30*time.Minute:
		return 5 * time.Second
	case elapsed < time.Hour:
		return time.Minute
	case elapsed < 2*time.Hour:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sleepUntilNextPoll(ctx context.Context, deadline time.Time, delay time.Duration) error {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	if delay > remaining {
		delay = remaining
	}
	return sleepWithContext(ctx, delay)
}
