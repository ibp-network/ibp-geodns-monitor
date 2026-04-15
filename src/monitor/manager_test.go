package monitor

import (
	"testing"
	"time"
)

func TestCheckQueueGetNextDropsStaleItems(t *testing.T) {
	queue := NewCheckQueue()
	stale := &CheckItem{
		Generation:      1,
		LastExecuted:    time.Now().Add(-time.Minute),
		MinimumInterval: 0,
	}
	current := &CheckItem{
		Generation:      2,
		LastExecuted:    time.Now().Add(-time.Minute),
		MinimumInterval: 0,
	}

	queue.Add(stale)
	queue.Add(current)

	got := queue.GetNext(2)
	if got != current {
		t.Fatalf("expected current generation item, got %#v", got)
	}
	if remaining := queue.Count(); remaining != 0 {
		t.Fatalf("expected queue to be empty after dropping stale items, got %d", remaining)
	}
}

func TestClaimNextItemReturnsNilWhileReloading(t *testing.T) {
	manager := &CheckManager{
		checkQueue: NewCheckQueue(),
		lastRuns:   make(map[string]time.Time),
	}
	manager.generation.Store(1)
	manager.reloading.Store(true)
	manager.checkQueue.Add(&CheckItem{
		Generation:      1,
		LastExecuted:    time.Now().Add(-time.Minute),
		MinimumInterval: 0,
	})

	if got := manager.claimNextItem(); got != nil {
		t.Fatalf("expected no claim while reloading, got %#v", got)
	}
	if remaining := manager.checkQueue.Count(); remaining != 1 {
		t.Fatalf("expected queued work to remain untouched, got %d items", remaining)
	}
}

func TestFinishItemSkipsRequeueDuringReload(t *testing.T) {
	manager := &CheckManager{
		checkQueue: NewCheckQueue(),
		lastRuns:   make(map[string]time.Time),
	}
	manager.generation.Store(1)
	manager.reloading.Store(true)

	item := &CheckItem{Generation: 1}
	manager.activeWG.Add(1)
	manager.finishItem(item)

	if remaining := manager.checkQueue.Count(); remaining != 0 {
		t.Fatalf("expected no requeue while reloading, got %d items", remaining)
	}
	if _, ok := manager.lastRuns[itemKey(item)]; !ok {
		t.Fatalf("expected last run to be recorded")
	}
}

func TestFinishItemRequeuesCurrentGeneration(t *testing.T) {
	manager := &CheckManager{
		checkQueue: NewCheckQueue(),
		lastRuns:   make(map[string]time.Time),
	}
	manager.generation.Store(3)

	item := &CheckItem{Generation: 3}
	manager.activeWG.Add(1)
	manager.finishItem(item)

	if remaining := manager.checkQueue.Count(); remaining != 1 {
		t.Fatalf("expected item to be requeued, got %d items", remaining)
	}
}
