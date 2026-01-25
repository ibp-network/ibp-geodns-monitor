package monitor

import (
	"reflect"
	"sync"
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
	lastConfig   cfg.Config
	generation   int
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

	return &CheckManager{
		workers:      make([]*Worker, numWorkers),
		checkQueue:   NewCheckQueue(),
		numWorkers:   numWorkers,
		separationMs: separationMs,
		shutdownCh:   make(chan struct{}),
		generation:   1,
		lastRuns:     make(map[string]time.Time),
		activeWG:     sync.WaitGroup{},
	}
}

func (cm *CheckManager) Start() {
	log.Log(log.Info, "Starting CheckManager with %d workers, %dms separation",
		cm.numWorkers, cm.separationMs)

	// Initialize all checks in the queue
	cm.lastConfig = cfg.GetConfig()
	cm.initializeChecks()

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
}

func (cm *CheckManager) Stop() {
	log.Log(log.Info, "Stopping CheckManager...")
	close(cm.shutdownCh)
	cm.wg.Wait()
	log.Log(log.Info, "CheckManager stopped")
}

func (cm *CheckManager) initializeChecks() {
	c := cfg.GetConfig()

	for _, check := range c.Local.Checks {
		if check.Enabled != 1 {
			continue
		}

		switch check.CheckType {
		case "site":
			cm.initializeSiteChecks(check)
		case "domain":
			cm.initializeDomainChecks(check)
		case "endpoint":
			cm.initializeEndpointChecks(check)
		}
	}

	log.Log(log.Info, "Initialized %d checks in queue", cm.checkQueue.Len())

	// Prune lastRuns to only current items
	cm.pruneLastRuns()
}

func (cm *CheckManager) initializeSiteChecks(check cfg.Check) {
	c := cfg.GetConfig()
	for _, member := range c.Members {
		if member.Service.Active == 1 && !member.Override {
			item := &CheckItem{
				Type:            "site",
				Check:           check,
				Member:          member,
				MinimumInterval: time.Duration(check.MinimumInterval) * time.Second,
				Generation:      cm.generation,
			}
			cm.applyLastExecuted(item)
			cm.checkQueue.Add(item)
		}
	}
}

func (cm *CheckManager) initializeDomainChecks(check cfg.Check) {
	c := cfg.GetConfig()
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
						Generation:      cm.generation,
					}
					cm.applyLastExecuted(item)
					cm.checkQueue.Add(item)
				}
			}
		}
	}
}

func (cm *CheckManager) initializeEndpointChecks(check cfg.Check) {
	c := cfg.GetConfig()
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
							Generation:      cm.generation,
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

	// Wait for in-flight checks to finish so we don't lose their last-run times
	cm.activeWG.Wait()

	// Bump generation to invalidate in-flight/stale items
	cm.generation++

	// Flush and rebuild the queue from the latest configuration
	cm.checkQueue.Clear()
	cm.initializeChecks()

	cm.lastConfig = currentCfg
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
	processed := 0
	for {
		if processed >= 1 {
			return
		}

		item := w.manager.checkQueue.GetNext(w.manager.generation)
		if item == nil {
			return
		}

		// Skip stale items from previous generations
		if item.Generation != w.manager.generation {
			continue
		}

		// Execute the check
		w.manager.activeWG.Add(1)
		w.executeCheck(item)
		w.manager.activeWG.Done()

		// Update last executed time
		item.LastExecuted = time.Now()
		w.manager.recordLastRun(item)

		// Re-queue the check only if generation is still current
		if item.Generation == w.manager.generation {
			w.manager.checkQueue.Add(item)
		}
		processed++
	}
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
	for _, it := range cm.checkQueue.items {
		if it == nil || it.Generation != cm.generation {
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
