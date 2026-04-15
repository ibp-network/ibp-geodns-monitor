package monitor

import (
	"sync"

	log "github.com/ibp-network/ibp-geodns-libs/logging"
)

var (
	manager   *CheckManager
	managerMu sync.Mutex
)

func Init() {
	log.Log(log.Debug, "Monitor Package initializing...")

	managerMu.Lock()
	current := manager
	manager = NewCheckManager()
	next := manager
	managerMu.Unlock()

	if current != nil {
		current.Stop()
	}

	next.Start()
}

func Shutdown() {
	managerMu.Lock()
	current := manager
	manager = nil
	managerMu.Unlock()

	if current != nil {
		current.Stop()
	}
}
