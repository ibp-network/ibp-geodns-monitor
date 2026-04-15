package monitor

import (
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	cfg "github.com/ibp-network/ibp-geodns-libs/config"
	log "github.com/ibp-network/ibp-geodns-libs/logging"
)

type CheckManager struct {
	workers      []*Worker
	checkQueue   *CheckQueue
	numWorkers   int
	separationMs int
	shutdownCh   chan struct{}
	wg           sync.WaitGroup
	startOnce    sync.Once
	stopOnce     sync.Once
	claimMu      sync.Mutex
	lastConfig   cfg.Config
	generation   atomic.Int64
	reloading    atomic.Bool
	lastRuns     map[string]time.Time
	lastRunsMu   sync.Mutex
	activeWG     sync.WaitGroup
}

type Worker struct {
	id         int
	manager    *CheckManager
	startDelay time.Duration
	ticker     *time.Ticker
}

func NewCheckManager() *CheckManager {
	c := cfg.GetConfig()
	numWorkers := c.Local.CheckWorkers.NumWorkers
	if numWorkers <= 0 {
		numWorkers = 10 // default
	}

	separationMs := c.Local.CheckWorkers.SeparationInterval
	if separationMs <= 0 {
		separationMs = 1000 // default 1 second
	}

	cm := &CheckManager{
		workers:      make([]*Worker, numWorkers),
		checkQueue:   NewCheckQueue(),
		numWorkers:   numWorkers,
		separationMs: separationMs,
		shutdownCh:   make(chan struct{}),
		lastRuns:     make(map[string]time.Time),
		activeWG:     sync.WaitGroup{},
	}
	cm.generation.Store(1)
	return cm
}

func (cm *CheckManager) Start() {
	cm.startOnce.Do(func() {
		log.Log(log.Info, "Starting CheckManager with %d workers, %dms separation",
			cm.numWorkers, cm.separationMs)

		// Initialize all checks in the queue from a single config snapshot.
		cm.lastConfig = cfg.GetConfig()
		cm.initializeChecks(cm.lastConfig)

		// Start workers with staggered delays
		for i := 0; i < cm.numWorkers; i++ {
			worker := &Worker{
				id:         i,
				manager:    cm,
				startDelay: time.Duration(i*cm.separationMs) * time.Millisecond,
			}
			cm.workers[i] = worker
			cm.wg.Add(1)
			go worker.run()
		}

		// Start the queue maintenance routine
		cm.wg.Add(1)
		go cm.maintainQueue()
	})
}

func (cm *CheckManager) Stop() {
	cm.stopOnce.Do(func() {
		log.Log(log.Info, "Stopping CheckManager...")
		close(cm.shutdownCh)
		cm.wg.Wait()
		log.Log(log.Info, "CheckManager stopped")
	})
}

func (cm *CheckManager) currentGeneration() int64 {
	return cm.generation.Load()
}

func (cm *CheckManager) initializeChecks(c cfg.Config) {
	for _, check := range c.Local.Checks {
		if check.Enabled != 1 {
			continue
		}

		switch check.CheckType {
		case "site":
			cm.initializeSiteChecks(c, check)
		case "domain":
			cm.initializeDomainChecks(c, check)
		case "endpoint":
			cm.initializeEndpointChecks(c, check)
		}
	}

	log.Log(log.Info, "Initialized %d checks in queue", cm.checkQueue.Count())

	// Prune lastRuns to only current items
	cm.pruneLastRuns()
}

func (cm *CheckManager) initializeSiteChecks(c cfg.Config, check cfg.Check) {
	for _, member := range c.Members {
		if member.Service.Active == 1 && !member.Override {
			item := &CheckItem{
				Type:            "site",
				Check:           check,
				Member:          member,
				MinimumInterval: time.Duration(check.MinimumInterval) * time.Second,
				Generation:      cm.currentGeneration(),
			}
			cm.applyLastExecuted(item)
			cm.checkQueue.Add(item)
		}
	}
}

func (cm *CheckManager) initializeDomainChecks(c cfg.Config, check cfg.Check) {
	for svcName, svc := range c.Services {
		if !isCheckValidForServiceType(check.Name, "domain", svc.Configuration.ServiceType) {
			continue
		}

		for _, mem := range c.Members {
			if mem.Service.Active == 1 && !mem.Override &&
				mem.Membership.Level >= svc.Configuration.LevelRequired &&
				assignedToService(svcName, mem) {

				domains := extractDomains(svc)
				for domain := range domains {
					item := &CheckItem{
						Type:            "domain",
						Check:           check,
						Member:          mem,
						Service:         svc,
						Domain:          domain,
						MinimumInterval: time.Duration(check.MinimumInterval) * time.Second,
						Generation:      cm.currentGeneration(),
					}
					cm.applyLastExecuted(item)
					cm.checkQueue.Add(item)
				}
			}
		}
	}
}

func (cm *CheckManager) initializeEndpointChecks(c cfg.Config, check cfg.Check) {
	for svcName, svc := range c.Services {
		if !isCheckValidForServiceType(check.Name, "endpoint", svc.Configuration.ServiceType) {
			continue
		}

		for _, mem := range c.Members {
			if mem.Service.Active == 1 && !mem.Override &&
				mem.Membership.Level >= svc.Configuration.LevelRequired &&
				assignedToService(svcName, mem) {

				for _, prov := range svc.Providers {
					for _, endpoint := range prov.RpcUrls {
						item := &CheckItem{
							Type:            "endpoint",
							Check:           check,
							Member:          mem,
							Service:         svc,
							Endpoint:        endpoint,
							Domain:          parseUrlForDomain(endpoint),
							MinimumInterval: time.Duration(check.MinimumInterval) * time.Second,
							Generation:      cm.currentGeneration(),
						}
						cm.applyLastExecuted(item)
						cm.checkQueue.Add(item)
					}
				}
			}
		}
	}
}

func (cm *CheckManager) maintainQueue() {
	defer cm.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Reload configuration and update checks if needed
			cm.updateChecksFromConfig()
		case <-cm.shutdownCh:
			return
		}
	}
}

func (cm *CheckManager) updateChecksFromConfig() {
	currentCfg := cfg.GetConfig()
	if reflect.DeepEqual(currentCfg, cm.lastConfig) {
		return // no change, skip reload
	}

	cm.claimMu.Lock()
	cm.reloading.Store(true)
	cm.claimMu.Unlock()

	// Wait for in-flight and already-claimed checks to finish so we don't lose their last-run times.
	cm.activeWG.Wait()

	// Bump generation to invalidate in-flight/stale items
	cm.generation.Add(1)

	// Flush and rebuild the queue from the latest configuration
	cm.checkQueue.Clear()
	cm.initializeChecks(currentCfg)

	cm.lastConfig = currentCfg
	cm.reloading.Store(false)
}

func (w *Worker) run() {
	defer w.manager.wg.Done()

	// Initial delay to stagger workers
	time.Sleep(w.startDelay)

	// Calculate interval: numWorkers * separationMs
	interval := time.Duration(w.manager.numWorkers*w.manager.separationMs) * time.Millisecond
	w.ticker = time.NewTicker(interval)
	defer w.ticker.Stop()

	log.Log(log.Debug, "Worker %d started with %v interval after %v delay",
		w.id, interval, w.startDelay)

	// Execute first check immediately after delay
	w.executeReadyChecks()

	for {
		select {
		case <-w.ticker.C:
			w.executeReadyChecks()
		case <-w.manager.shutdownCh:
			return
		}
	}
}

func (w *Worker) executeReadyChecks() {
	item := w.manager.claimNextItem()
	if item == nil {
		return
	}

	w.executeCheck(item)
	w.manager.finishItem(item)
}

func (cm *CheckManager) applyLastExecuted(item *CheckItem) {
	cm.lastRunsMu.Lock()
	defer cm.lastRunsMu.Unlock()
	if t, ok := cm.lastRuns[itemKey(item)]; ok {
		item.LastExecuted = t
	}
}

func itemKey(it *CheckItem) string {
	return it.Type + "|" + it.Check.Name + "|" + it.Member.Details.Name + "|" + it.Domain + "|" + it.Endpoint +
		"|" + it.Member.Service.ServiceIPv4 + "|" + it.Member.Service.ServiceIPv6
}

func (cm *CheckManager) recordLastRun(item *CheckItem) {
	cm.lastRunsMu.Lock()
	defer cm.lastRunsMu.Unlock()
	cm.lastRuns[itemKey(item)] = item.LastExecuted
}

func (cm *CheckManager) pruneLastRuns() {
	cm.lastRunsMu.Lock()
	defer cm.lastRunsMu.Unlock()

	valid := make(map[string]struct{})
	currentGeneration := cm.currentGeneration()
	for _, it := range cm.checkQueue.Snapshot() {
		if it == nil || it.Generation != currentGeneration {
			continue
		}
		valid[itemKey(it)] = struct{}{}
	}

	for k := range cm.lastRuns {
		if _, ok := valid[k]; !ok {
			delete(cm.lastRuns, k)
		}
	}
}

func (cm *CheckManager) claimNextItem() *CheckItem {
	cm.claimMu.Lock()
	defer cm.claimMu.Unlock()

	if cm.reloading.Load() {
		return nil
	}

	item := cm.checkQueue.GetNext(cm.currentGeneration())
	if item != nil {
		cm.activeWG.Add(1)
	}
	return item
}

func (cm *CheckManager) finishItem(item *CheckItem) {
	item.LastExecuted = time.Now()
	cm.recordLastRun(item)

	if !cm.reloading.Load() && item.Generation == cm.currentGeneration() {
		cm.checkQueue.Add(item)
	}

	cm.activeWG.Done()
}

func (w *Worker) executeCheck(item *CheckItem) {
	defer func() {
		if r := recover(); r != nil {
			log.Log(log.Error, "Worker %d: Check panic for %s/%s: %v",
				w.id, item.Check.Name, item.Member.Details.Name, r)
		}
	}()

	switch item.Type {
	case "site":
		if fn, ok := getSiteCheck(item.Check.Name); ok {
			fn(item.Check, item.Member)
		}
	case "domain":
		if fn, ok := getDomainCheck(item.Check.Name); ok {
			fn(item.Check, item.Domain, item.Service, item.Member)
		}
	case "endpoint":
		if fn, ok := getEndpointCheck(item.Check.Name); ok {
			fn(item.Check, item.Endpoint, item.Service, item.Member)
		}
	}
}
