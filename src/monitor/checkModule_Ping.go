package monitor

import (
	"fmt"
	"strings"
	"time"

	cfg "github.com/ibp-network/ibp-geodns-libs/config"
	log "github.com/ibp-network/ibp-geodns-libs/logging"

	"github.com/go-ping/ping"
)

func init() {
	RegisterSiteCheck("ping", PingCheck)
}

func PingCheck(check cfg.Check, member cfg.Member) {
	if member.Service.ServiceIPv4 == "" && member.Service.ServiceIPv6 == "" {
		UpdateSiteResultLocal(check, member, false, "No IPv4 or IPv6 configured", nil, false)
		return
	}

	if member.Service.ServiceIPv4 != "" {
		runPingSingle(check, member, false)
	}

	if member.Service.ServiceIPv6 != "" {
		runPingSingle(check, member, true)
	}
}

func runPingSingle(check cfg.Check, member cfg.Member, isIPv6 bool) {
	var ipToPing string
	if isIPv6 {
		ipToPing = member.Service.ServiceIPv6
	} else {
		ipToPing = member.Service.ServiceIPv4
	}

	pingCount := getIntOption(check.ExtraOptions, "PingCount", 3)
	pingInterval := time.Duration(getIntOption(check.ExtraOptions, "PingInterval", 100)) * time.Millisecond
	pingTimeout := time.Duration(getIntOption(check.ExtraOptions, "PingTimeout", 1000)) * time.Millisecond
	pingSize := getIntOption(check.ExtraOptions, "PingSize", 32)
	pingTTL := getIntOption(check.ExtraOptions, "PingTTL", 64)
	maxPacketLoss := getFloatOption(check.ExtraOptions, "MaxPacketLoss", 5.0)
	maxLatency := int64(getIntOption(check.ExtraOptions, "MaxLatency", 800))

	options := pingOptions{
		Count:       pingCount,
		Interval:    pingInterval,
		Timeout:     pingTimeout,
		Size:        pingSize,
		TTL:         pingTTL,
		MaxLoss:     maxPacketLoss,
		MaxLatency:  maxLatency,
	}
	stats, err := runPing(ipToPing, isIPv6, options)
	if err != nil {
		UpdateSiteResultLocal(check, member, false, err.Error(), nil, isIPv6)
		return
	}

	success := stats.PacketsRecv > 0 && stats.PacketLoss <= options.MaxLoss && stats.AvgRtt.Milliseconds() <= options.MaxLatency
	var msg string
	if !success {
		msg = fmt.Sprintf("PingCheck: avgRtt=%dms, loss=%.0f%%", stats.AvgRtt.Milliseconds(), stats.PacketLoss)
	}
	dataMap := map[string]interface{}{
		"PacketLoss": stats.PacketLoss,
		"MinRtt":     stats.MinRtt.Milliseconds(),
		"AvgRtt":     stats.AvgRtt.Milliseconds(),
		"MaxRtt":     stats.MaxRtt.Milliseconds(),
		"StdDevRtt":  stats.StdDevRtt.Milliseconds(),
	}

	UpdateSiteResultLocal(check, member, success, msg, dataMap, isIPv6)
	log.Log(log.Debug, "Ping check completed for %s isIPv6=%v success=%v", member.Details.Name, isIPv6, success)
}

type pingOptions struct {
	Count      int
	Interval   time.Duration
	Timeout    time.Duration
	Size       int
	TTL        int
	MaxLoss    float64
	MaxLatency int64
}

func runPing(ipToPing string, isIPv6 bool, options pingOptions) (*ping.Statistics, error) {
	stats, err := executePing(ipToPing, isIPv6, options, true)
	if err != nil && isPrivilegedPingError(err) {
		log.Log(log.Warn, "Privileged ping failed for %s, retrying unprivileged: %v", ipToPing, err)
		return executePing(ipToPing, isIPv6, options, false)
	}
	return stats, err
}

func executePing(ipToPing string, isIPv6 bool, options pingOptions, privileged bool) (*ping.Statistics, error) {
	pinger, err := ping.NewPinger(ipToPing)
	if err != nil {
		return nil, fmt.Errorf("ping error init: %w", err)
	}
	if isIPv6 {
		pinger.SetNetwork("ip6")
	}

	pinger.Count = options.Count
	pinger.Interval = options.Interval
	pinger.Timeout = options.Timeout * time.Duration(options.Count)
	pinger.Size = options.Size
	pinger.TTL = options.TTL
	pinger.SetPrivileged(privileged)

	if err := pinger.Run(); err != nil {
		return nil, err
	}
	return pinger.Statistics(), nil
}

func isPrivilegedPingError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission") ||
		strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "raw socket")
}
