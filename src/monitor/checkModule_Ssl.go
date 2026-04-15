package monitor

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"

	cfg "github.com/ibp-network/ibp-geodns-libs/config"
	log "github.com/ibp-network/ibp-geodns-libs/logging"
)

func init() {
	// SSL check is valid for both RPC and ETHRPC service types
	RegisterDomainCheckWithTypes("ssl", SslCheck, []string{"RPC", "ETHRPC"})
}

func SslCheck(check cfg.Check, domain string, service cfg.Service, member cfg.Member) {
	target, err := parseCheckTarget(domain, "https")
	if err != nil {
		UpdateDomainResultLocal(check, domain, service, member, false,
			fmt.Sprintf("Invalid TLS target: %v", err), nil, false)
		return
	}

	ip4 := member.Service.ServiceIPv4
	ip6 := member.Service.ServiceIPv6
	if ip4 == "" && ip6 == "" {
		UpdateDomainResultLocal(check, target.Label, service, member, false,
			"No IPv4 or IPv6 configured", nil, false)
		return
	}
	if ip4 != "" {
		dialAndCheckTLS(check, target, service, member, ip4, false)
	}
	if ip6 != "" {
		dialAndCheckTLS(check, target, service, member, ip6, true)
	}
}

func dialAndCheckTLS(
	check cfg.Check,
	target CheckTarget,
	service cfg.Service,
	member cfg.Member,
	ip string,
	isIPv6 bool,
) {
	timeoutSec := getIntOption(check.ExtraOptions, "ConnectTimeout", 5)
	timeout := time.Duration(timeoutSec) * time.Second
	conn, err := net.DialTimeout("tcp", target.DialAddress(ip), timeout)
	if err != nil {
		UpdateDomainResultLocal(check, target.Label, service, member, false,
			fmt.Sprintf("TCP connect error: %v", err), nil, isIPv6)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         target.Hostname,
		InsecureSkipVerify: false,
	})
	err = tlsConn.Handshake()
	if err != nil {
		UpdateDomainResultLocal(check, target.Label, service, member, false,
			fmt.Sprintf("TLS handshake failed: %v", err), nil, isIPv6)
		return
	}
	defer tlsConn.Close()

	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		UpdateDomainResultLocal(check, target.Label, service, member, false, "No certificate found", nil, isIPv6)
		return
	}

	cert := certs[0]
	daysUntilExpiry := int(time.Until(cert.NotAfter).Hours() / 24)
	success := true
	errText := ""
	if daysUntilExpiry < 5 {
		success = false
		errText = "Less than 5 days to expiry"
	}

	dataMap := map[string]interface{}{
		"ExpiryTimestamp": cert.NotAfter.Unix(),
		"DaysUntilExpiry": daysUntilExpiry,
		"Port":            target.Port,
	}

	if success {
		UpdateDomainResultLocal(check, target.Label, service, member, true, "", dataMap, isIPv6)
		log.Log(log.Debug, "SSL check completed for %s %s isIPv6=%v success=%v", member.Details.Name, target.Label, isIPv6, true)
	} else {
		UpdateDomainResultLocal(check, target.Label, service, member, false, errText, dataMap, isIPv6)
		log.Log(log.Debug, "SSL check failed for %s %s isIPv6=%v success=%v", member.Details.Name, target.Label, isIPv6, false)
	}
}
