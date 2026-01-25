package monitor

import (
	"container/heap"
	"sync"
	"time"

	cfg "github.com/ibp-network/ibp-geodns-libs/config"
)

type CheckItem struct {
	Type            string // "site", "domain", "endpoint"
	Check           cfg.Check
	Member          cfg.Member
	Service         cfg.Service
	Domain          string
	Endpoint        string
	LastExecuted    time.Time
	MinimumInterval time.Duration
	index           int // Used by heap
}

type CheckQueue struct {
	mu    sync.Mutex
	items []*CheckItem
}

func NewCheckQueue() *CheckQueue {
	cq := &CheckQueue{
		items: make([]*CheckItem, 0),
	}
	heap.Init(cq)
	return cq
}

// Priority queue implementation (heap.Interface)
func (cq *CheckQueue) Len() int { return len(cq.items) }

func (cq *CheckQueue) Less(i, j int) bool {
	// Earlier next run time has higher priority; no dependency on current time
	iNext := cq.items[i].LastExecuted.Add(cq.items[i].MinimumInterval)
	jNext := cq.items[j].LastExecuted.Add(cq.items[j].MinimumInterval)
	return iNext.Before(jNext)
}

func (cq *CheckQueue) Swap(i, j int) {
	cq.items[i], cq.items[j] = cq.items[j], cq.items[i]
	cq.items[i].index = i
	cq.items[j].index = j
}

// These are required by heap.Interface - they use interface{}
func (cq *CheckQueue) Push(x interface{}) {
	item := x.(*CheckItem)
	item.index = len(cq.items)
	cq.items = append(cq.items, item)
}

func (cq *CheckQueue) Pop() interface{} {
	old := cq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	cq.items = old[0 : n-1]
	return item
}

// Thread-safe public methods with different names
func (cq *CheckQueue) Add(item *CheckItem) {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	heap.Push(cq, item)
}

func (cq *CheckQueue) GetNext() *CheckItem {
	cq.mu.Lock()
	defer cq.mu.Unlock()

	if cq.Len() == 0 {
		return nil
	}

	// Check the top of the heap; if it's not ready, none are ready
	item := cq.items[0]
	nextRun := item.LastExecuted.Add(item.MinimumInterval)
	now := time.Now()
	if now.Before(nextRun) {
		return nil
	}

	// Pop the ready item
	return heap.Pop(cq).(*CheckItem)
}
