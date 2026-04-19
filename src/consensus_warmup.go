package main

import "time"

const (
	monitorPeerDiscoveryTarget   = 2
	monitorPeerDiscoveryTimeout  = 3 * time.Second
	monitorPeerDiscoveryInterval = 100 * time.Millisecond
)

func waitForMonitorPeerDiscovery(timeout, interval time.Duration, countActive func() int) int {
	if countActive == nil {
		return 0
	}

	active := countActive()
	if active >= monitorPeerDiscoveryTarget || timeout <= 0 {
		return active
	}
	if interval <= 0 {
		interval = monitorPeerDiscoveryInterval
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for active < monitorPeerDiscoveryTarget {
		if time.Now().After(deadline) {
			break
		}
		<-ticker.C
		active = countActive()
	}

	return active
}
