package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	cfg "github.com/ibp-network/ibp-geodns-libs/config"
	log "github.com/ibp-network/ibp-geodns-libs/logging"
)

type EthRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type EthRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func init() {
	// ETHRPC check is only valid for ETHRPC service type
	RegisterEndpointCheckWithTypes("ethrpc", EthrpcCheck, []string{"ETHRPC"})
}

func EthrpcCheck(check cfg.Check, endpoint string, service cfg.Service, member cfg.Member) {
	target, err := parseCheckTarget(endpoint, "https")
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, fmt.Sprintf("Invalid HTTP RPC target: %v", err), nil, false)
		return
	}
	target.Scheme = httpSchemeForTarget(target.Scheme)
	target.URL = buildTargetURL(target.Scheme, target.Hostname, target.Port, target.RequestURI)

	ip4 := member.Service.ServiceIPv4
	ip6 := member.Service.ServiceIPv6

	if ip4 == "" && ip6 == "" {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, "No IPv4 or IPv6 configured", nil, false)
		return
	}

	if ip4 != "" {
		runEthrpcSingle(check, endpoint, target, service, member, ip4, false)
	}
	if ip6 != "" {
		runEthrpcSingle(check, endpoint, target, service, member, ip6, true)
	}
}

func runEthrpcSingle(check cfg.Check, endpoint string, target CheckTarget, service cfg.Service, member cfg.Member, ip string, isIPv6 bool) {
	log.Log(log.Debug, "ETHRPC check: endpoint=%s => url=%s (connecting via %s) for %s",
		endpoint, target.URL, ip, member.Details.Name)

	// Create HTTP client with custom transport that redirects to IP
	timeoutSec := getIntOption(check.ExtraOptions, "ConnectTimeout", 10)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: target.Hostname,
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// addr will be "domain:port", we replace with "ip:port"
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				port = target.Port
			}

			log.Log(log.Debug, "ETHRPC dial: intercepting %s:%s => %s:%s", host, port, ip, port)

			dialer := &net.Dialer{
				Timeout: time.Duration(timeoutSec) * time.Second,
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
		},
	}

	client := &http.Client{
		Timeout:   time.Duration(timeoutSec) * time.Second,
		Transport: transport,
	}

	// Test 1: Check eth_chainId
	chainId, err := ethCall(client, target.URL, "eth_chainId", []interface{}{})
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			fmt.Sprintf("eth_chainId failed: %v", err), nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - eth_chainId error: %v",
			member.Details.Name, endpoint, isIPv6, err)
		return
	}

	// Test 2: Check eth_blockNumber
	blockNumber, err := ethCall(client, target.URL, "eth_blockNumber", []interface{}{})
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			fmt.Sprintf("eth_blockNumber failed: %v", err), nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - eth_blockNumber error",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	// Test 3: Check net_version
	netVersion, err := ethCall(client, target.URL, "net_version", []interface{}{})
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			fmt.Sprintf("net_version failed: %v", err), nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - net_version error",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	// Test 4: Check eth_syncing - must be false
	syncingResult, err := ethCall(client, target.URL, "eth_syncing", []interface{}{})
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			fmt.Sprintf("eth_syncing failed: %v", err), nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - eth_syncing error",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	syncing, err := parseEthSyncing(syncingResult)
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			fmt.Sprintf("Invalid eth_syncing response: %v", err), nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - invalid eth_syncing response",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	if syncing {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			"Node is syncing", nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - Node is syncing",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	// Parse responses
	var chainIdStr string
	if err := json.Unmarshal(chainId, &chainIdStr); err != nil || strings.TrimSpace(chainIdStr) == "" {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			"Invalid eth_chainId response", nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - invalid eth_chainId response",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	var blockNumberStr string
	if err := json.Unmarshal(blockNumber, &blockNumberStr); err != nil || strings.TrimSpace(blockNumberStr) == "" {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			"Invalid eth_blockNumber response", nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - invalid eth_blockNumber response",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	var netVersionStr string
	if err := json.Unmarshal(netVersion, &netVersionStr); err != nil || strings.TrimSpace(netVersionStr) == "" {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			"Invalid net_version response", nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - invalid net_version response",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	// Verify chain matches expected network
	expectedNetwork := strings.TrimSpace(service.Configuration.NetworkName)

	chainIdDecimal, chainIDValid := parseChainID(chainIdStr)

	// Check if network matches - compare both net_version and chainId
	networkMatches := matchesEthNetwork(expectedNetwork, netVersionStr, chainIdStr, chainIdDecimal, chainIDValid)

	log.Log(log.Debug, "ETHRPC network check for %s: expected=%s, chainId=%s, chainIdDec=%d, netVersion=%s, matches=%v",
		member.Details.Name, expectedNetwork, chainIdStr, chainIdDecimal, netVersionStr, networkMatches)

	if !networkMatches {
		UpdateEndpointResultLocal(check, member, service, endpoint, false,
			fmt.Sprintf("Wrong network: expected %s, got net_version=%s chainId=%s (decimal=%d)",
				expectedNetwork, netVersionStr, chainIdStr, chainIdDecimal), nil, isIPv6)
		log.Log(log.Debug, "ETHRPC check failed for %s %s isIPv6=%v - Wrong network",
			member.Details.Name, endpoint, isIPv6)
		return
	}

	// All checks passed
	dataMap := map[string]interface{}{
		"chainId":     chainIdStr,
		"chainIdDec":  chainIdDecimal,
		"blockNumber": blockNumberStr,
		"netVersion":  netVersionStr,
		"syncing":     false,
		"network":     expectedNetwork,
	}

	UpdateEndpointResultLocal(check, member, service, endpoint, true, "", dataMap, isIPv6)
	log.Log(log.Debug, "ETHRPC check completed for %s %s isIPv6=%v success=%v",
		member.Details.Name, endpoint, isIPv6, true)
}

func parseEthSyncing(result json.RawMessage) (bool, error) {
	payload := strings.TrimSpace(string(result))
	switch strings.ToLower(payload) {
	case "false", "null":
		return false, nil
	case "true":
		return true, nil
	}

	var syncing bool
	if err := json.Unmarshal(result, &syncing); err == nil {
		return syncing, nil
	}

	var syncObject map[string]interface{}
	if err := json.Unmarshal(result, &syncObject); err == nil && syncObject != nil {
		return true, nil
	}

	var syncString string
	if err := json.Unmarshal(result, &syncString); err == nil {
		switch strings.ToLower(strings.TrimSpace(syncString)) {
		case "false", "":
			return false, nil
		case "true":
			return true, nil
		}
	}

	return false, fmt.Errorf("unexpected payload %s", payload)
}

func parseChainID(chainID string) (int64, bool) {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		return 0, false
	}

	if strings.HasPrefix(strings.ToLower(chainID), "0x") {
		out, err := strconv.ParseInt(chainID[2:], 16, 64)
		return out, err == nil
	}

	out, err := strconv.ParseInt(chainID, 10, 64)
	return out, err == nil
}

func matchesEthNetwork(expectedNetwork, netVersion, chainID string, chainIDDecimal int64, chainIDValid bool) bool {
	expectedNetwork = strings.TrimSpace(expectedNetwork)
	if expectedNetwork == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(netVersion), expectedNetwork) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(chainID), expectedNetwork) {
		return true
	}
	if chainIDValid && fmt.Sprintf("%d", chainIDDecimal) == expectedNetwork {
		return true
	}
	return false
}

func httpSchemeForTarget(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "ws":
		return "http"
	default:
		return "https"
	}
}

func ethCall(client *http.Client, url string, method string, params []interface{}) (json.RawMessage, error) {
	request := EthRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp EthRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}
