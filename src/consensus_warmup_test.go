package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForMonitorPeerDiscoveryReturnsImmediatelyWhenPeerAlreadyVisible(t *testing.T) {
	start := time.Now()
	active := waitForMonitorPeerDiscovery(200*time.Millisecond, 10*time.Millisecond, func() int { return 2 })
	if active != 2 {
		t.Fatalf("expected active monitor count 2, got %d", active)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("expected immediate return, took %v", elapsed)
	}
}

func TestWaitForMonitorPeerDiscoveryWaitsForSecondMonitor(t *testing.T) {
	var active atomic.Int32
	active.Store(1)

	go func() {
		time.Sleep(20 * time.Millisecond)
		active.Store(2)
	}()

	start := time.Now()
	got := waitForMonitorPeerDiscovery(250*time.Millisecond, 5*time.Millisecond, func() int {
		return int(active.Load())
	})
	if got != 2 {
		t.Fatalf("expected warmup to observe 2 active monitors, got %d", got)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("expected warmup to wait for peer discovery, returned too early in %v", elapsed)
	}
}

func TestWaitForMonitorPeerDiscoveryTimesOutWithCurrentCount(t *testing.T) {
	start := time.Now()
	got := waitForMonitorPeerDiscovery(30*time.Millisecond, 5*time.Millisecond, func() int { return 1 })
	if got != 1 {
		t.Fatalf("expected timeout to return current active monitor count, got %d", got)
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Fatalf("expected warmup to wait close to timeout, took %v", elapsed)
	}
}
