package monitor

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	cfg "github.com/ibp-network/ibp-geodns-libs/config"
	log "github.com/ibp-network/ibp-geodns-libs/logging"

	"github.com/gorilla/websocket"
)

type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func init() {
	// WSS check is only valid for RPC service type
	RegisterEndpointCheckWithTypes("wss", WssCheck, []string{"RPC"})
}

func WssCheck(check cfg.Check, endpoint string, service cfg.Service, member cfg.Member) {
	target, err := parseCheckTarget(endpoint, "wss")
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, fmt.Sprintf("Invalid WebSocket target: %v", err), nil, false)
		return
	}
	target.Scheme = websocketSchemeForTarget(target.Scheme)
	target.URL = buildTargetURL(target.Scheme, target.Hostname, target.Port, target.RequestURI)

	ip4 := member.Service.ServiceIPv4
	ip6 := member.Service.ServiceIPv6
	readTimeoutSec := getIntOption(check.ExtraOptions, "ReadTimeout", 15)

	if ip4 == "" && ip6 == "" {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, "No IPv4 or IPv6 configured", nil, false)
		return
	}

	if ip4 != "" {
		runWssSingle(check, endpoint, target, service, member, ip4, false, readTimeoutSec)
	}

	if ip6 != "" {
		runWssSingle(check, endpoint, target, service, member, ip6, true, readTimeoutSec)
	}
}

func runWssSingle(check cfg.Check, endpoint string, target CheckTarget, service cfg.Service, member cfg.Member, ip string, isIPv6 bool, readTimeoutSec int) {
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			ServerName:         target.Hostname,
			InsecureSkipVerify: false,
		},
		NetDial: func(network, addr string) (net.Conn, error) {
			return net.DialTimeout(network, target.DialAddress(ip),
				time.Duration(getIntOption(check.ExtraOptions, "ConnectTimeout", 10))*time.Second)
		},
		HandshakeTimeout: time.Duration(getIntOption(check.ExtraOptions, "ConnectTimeout", 10)) * time.Second,
	}

	c, _, err := dialer.Dial(target.URL, nil)
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, fmt.Sprintf("Failed to connect on IP=%s => %v", ip, err), nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}
	defer c.Close()

	request := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "chain_getBlockHash",
		Params:  []interface{}{},
		ID:      1,
	}

	if !sendJSONRPCRequest(c, request) {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, "Failed to send JSON RPC", nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}

	var latestBlockHash string
	if err := readJSONRPCResult(c, readTimeoutSec, "chain_getBlockHash()", &latestBlockHash); err != nil || latestBlockHash == "" {
		errText := "chain_getBlockHash() returned an empty result"
		if err != nil {
			errText = err.Error()
		}
		UpdateEndpointResultLocal(check, member, service, endpoint, false, errText, nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}

	isFullArchive, err := checkFullArchive(c, readTimeoutSec)
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, fmt.Sprintf("Full archive check failed: %v", err), nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}

	if !isFullArchive {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, "Not a full archive node", nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}

	isCorrectNetwork, err := checkNetwork(c, service.Configuration.NetworkName, service.Configuration.StateRootHash, readTimeoutSec)
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, fmt.Sprintf("Network check failed: %v", err), nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}

	if !isCorrectNetwork {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, "Wrong network", nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}

	minPeers := getIntOption(check.ExtraOptions, "MinimumPeers", 5)
	hasEnoughPeers, isSyncing, peerCount, err := checkPeers(c, readTimeoutSec, minPeers)
	if err != nil {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, fmt.Sprintf("Peer check failed: %v", err), nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}

	if !hasEnoughPeers || isSyncing {
		UpdateEndpointResultLocal(check, member, service, endpoint, false, "Syncing or not enough peers", nil, isIPv6)
		log.Log(log.Debug, "WSS check failed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, false)
		return
	}

	log.Log(log.Debug, "WSS check completed for %s %s isIPv6=%v success=%v", member.Details.Name, endpoint, isIPv6, true)
	UpdateEndpointResultLocal(check, member, service, endpoint, true, "",
		map[string]interface{}{
			"Syncing":   isSyncing,
			"Peers":     hasEnoughPeers,
			"PeerCount": peerCount,
			"Network":   isCorrectNetwork,
			"Archive":   isFullArchive,
		}, isIPv6)
}

// The rest is unchanged
func checkFullArchive(c *websocket.Conn, readTimeoutSec int) (bool, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "chain_getBlockHash",
		Params:  []interface{}{0},
		ID:      2,
	}

	if !sendJSONRPCRequest(c, req) {
		return false, fmt.Errorf("failed to send blockHash(0) request")
	}

	var result string
	if err := readJSONRPCResult(c, readTimeoutSec, "chain_getBlockHash(0)", &result); err != nil {
		return false, err
	}

	if result == "" {
		return false, fmt.Errorf("invalid chain_getBlockHash(0) response")
	}

	return true, nil
}

func checkNetwork(c *websocket.Conn, expectedNetwork string, expectedStateRootHash string, readTimeoutSec int) (bool, error) {
	// First check the chain name
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "system_chain",
		ID:      3,
	}

	if !sendJSONRPCRequest(c, req) {
		return false, fmt.Errorf("failed to send system_chain request")
	}

	var chain string
	if err := readJSONRPCResult(c, readTimeoutSec, "system_chain", &chain); err != nil {
		return false, err
	}

	if !strings.EqualFold(chain, expectedNetwork) {
		return false, nil
	}

	// If StateRootHash is not configured, skip the check
	if expectedStateRootHash == "" {
		log.Log(log.Debug, "StateRootHash not configured for network %s, skipping check", expectedNetwork)
		return true, nil
	}

	// Check the state root hash of the genesis block (block 0)
	// Get block hash at height 0
	blockHashReq := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "chain_getBlockHash",
		Params:  []interface{}{0},
		ID:      4,
	}

	if !sendJSONRPCRequest(c, blockHashReq) {
		return false, fmt.Errorf("failed to send chain_getBlockHash(0) for genesis block")
	}

	var genesisBlockHash string
	if err := readJSONRPCResult(c, readTimeoutSec, "chain_getBlockHash(0)", &genesisBlockHash); err != nil {
		return false, fmt.Errorf("failed to read genesis block hash response: %v", err)
	}
	if genesisBlockHash == "" {
		return false, fmt.Errorf("invalid genesis block hash response")
	}

	// Get genesis block header to extract state root
	headerReq := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "chain_getHeader",
		Params:  []interface{}{genesisBlockHash},
		ID:      5,
	}

	if !sendJSONRPCRequest(c, headerReq) {
		return false, fmt.Errorf("failed to send chain_getHeader request for genesis block")
	}

	// Extract state root from genesis block header
	header := make(map[string]interface{})
	if err := readJSONRPCResult(c, readTimeoutSec, "chain_getHeader(genesis)", &header); err != nil {
		return false, fmt.Errorf("failed to read genesis header response: %v", err)
	}

	genesisStateRoot, ok := header["stateRoot"].(string)
	if !ok {
		return false, fmt.Errorf("state root not found in genesis header")
	}

	// Compare genesis state root with expected
	if !strings.EqualFold(genesisStateRoot, expectedStateRootHash) {
		log.Log(log.Warn, "Genesis state root mismatch for %s: expected %s, got %s", expectedNetwork, expectedStateRootHash, genesisStateRoot)
		return false, fmt.Errorf("genesis state root mismatch: expected %s, got %s", expectedStateRootHash, genesisStateRoot)
	}

	log.Log(log.Debug, "Genesis state root hash verified for %s: %s", expectedNetwork, genesisStateRoot)
	return true, nil
}

func checkPeers(c *websocket.Conn, readTimeoutSec int, minPeers int) (bool, bool, int64, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "system_health",
		ID:      6,
	}

	if !sendJSONRPCRequest(c, req) {
		return false, false, 0, fmt.Errorf("failed to send system_health request")
	}

	result := make(map[string]interface{})
	if err := readJSONRPCResult(c, readTimeoutSec, "system_health", &result); err != nil {
		return false, false, 0, err
	}

	peers, ok := parseFlexibleInt(result["peers"])
	if !ok {
		return false, false, 0, fmt.Errorf("invalid peers field")
	}

	syncing, ok := result["isSyncing"].(bool)
	if !ok {
		return false, false, 0, fmt.Errorf("invalid isSyncing field")
	}

	hasEnoughPeers := peers >= int64(minPeers)

	return hasEnoughPeers, syncing, peers, nil
}

func sendJSONRPCRequest(c *websocket.Conn, request JSONRPCRequest) bool {
	data, err := json.Marshal(request)
	if err != nil {
		return false
	}

	if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
		return false
	}

	return true
}

func readWithDeadline(c *websocket.Conn, timeoutSec int, desc string) ([]byte, error) {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	if err := c.SetReadDeadline(time.Now().Add(time.Duration(timeoutSec) * time.Second)); err != nil {
		return nil, fmt.Errorf("%s: failed to set read deadline: %w", desc, err)
	}
	_, message, err := c.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", desc, err)
	}
	return message, nil
}

func readJSONRPCResponse(c *websocket.Conn, timeoutSec int, desc string) (JSONRPCResponse, error) {
	message, err := readWithDeadline(c, timeoutSec, desc)
	if err != nil {
		return JSONRPCResponse{}, err
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(message, &resp); err != nil {
		return JSONRPCResponse{}, fmt.Errorf("%s: failed to decode response: %w", desc, err)
	}
	if resp.Error != nil {
		return JSONRPCResponse{}, fmt.Errorf("%s: rpc error %d: %s", desc, resp.Error.Code, resp.Error.Message)
	}
	if len(resp.Result) == 0 {
		return JSONRPCResponse{}, fmt.Errorf("%s: missing result", desc)
	}
	return resp, nil
}

func readJSONRPCResult(c *websocket.Conn, timeoutSec int, desc string, target interface{}) error {
	resp, err := readJSONRPCResponse(c, timeoutSec, desc)
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Result, target); err != nil {
		return fmt.Errorf("%s: invalid result: %w", desc, err)
	}
	return nil
}

func websocketSchemeForTarget(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "ws":
		return "ws"
	default:
		return "wss"
	}
}
