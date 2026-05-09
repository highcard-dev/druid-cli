package kubernetes

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJobPollIntervalTiers(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		want    time.Duration
	}{
		{name: "before thirty minutes", elapsed: 29*time.Minute + 59*time.Second, want: 5 * time.Second},
		{name: "at thirty minutes", elapsed: 30 * time.Minute, want: time.Minute},
		{name: "before one hour", elapsed: 59*time.Minute + 59*time.Second, want: time.Minute},
		{name: "at one hour", elapsed: time.Hour, want: 2 * time.Minute},
		{name: "before two hours", elapsed: 2*time.Hour - time.Second, want: 2 * time.Minute},
		{name: "at two hours", elapsed: 2 * time.Hour, want: 5 * time.Minute},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := jobPollInterval(test.elapsed); got != test.want {
				t.Fatalf("jobPollInterval(%s) = %s, want %s", test.elapsed, got, test.want)
			}
		})
	}
}

func TestCappedBackoffIncreasesAndCaps(t *testing.T) {
	backoff := newCappedBackoff(time.Second, 2*time.Second)
	if got := backoff.Next(); got != time.Second {
		t.Fatalf("first delay = %s, want 1s", got)
	}
	if got := backoff.Next(); got != 1250*time.Millisecond {
		t.Fatalf("second delay = %s, want 1.25s", got)
	}
	for i := 0; i < 10; i++ {
		_ = backoff.Next()
	}
	if got := backoff.Next(); got != 2*time.Second {
		t.Fatalf("capped delay = %s, want 2s", got)
	}
}

func TestSleepWithContextReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startedAt := time.Now()
	err := sleepWithContext(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepWithContext error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("sleepWithContext returned after %s, want immediate cancellation", elapsed)
	}
}
