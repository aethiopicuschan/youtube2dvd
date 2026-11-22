package main

import (
	"testing"
	"time"
)

func TestNextEnvironmentCheckDelay(t *testing.T) {
	online := environmentCheck{internet: "接続あり"}
	offline := environmentCheck{internet: "未接続"}

	failures := 0
	var delays []time.Duration
	for range environmentQuickRetries + 2 {
		var delay time.Duration
		delay, failures = nextEnvironmentCheckDelay(offline, failures)
		delays = append(delays, delay)
	}
	for i, delay := range delays {
		want := environmentRetryInterval
		if i >= environmentQuickRetries {
			want = environmentCheckInterval
		}
		if delay != want {
			t.Errorf("delay after failure %d = %v, want %v", i+1, delay, want)
		}
	}

	delay, failures := nextEnvironmentCheckDelay(online, failures)
	if delay != environmentCheckInterval || failures != 0 {
		t.Errorf("after reconnecting: delay %v, failures %d; want %v, 0", delay, failures, environmentCheckInterval)
	}
	// A new outage gets quick retries again.
	if delay, _ := nextEnvironmentCheckDelay(offline, failures); delay != environmentRetryInterval {
		t.Errorf("first failure after reconnecting: delay %v, want %v", delay, environmentRetryInterval)
	}
}
