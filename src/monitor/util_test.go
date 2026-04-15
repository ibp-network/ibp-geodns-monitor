package monitor

import (
	"encoding/json"
	"testing"
)

func TestParseCheckTargetPreservesNonDefaultPortAndPath(t *testing.T) {
	target, err := parseCheckTarget("WSS://RPC.EXAMPLE.com:8443/ws/v1?foo=bar", "wss")
	if err != nil {
		t.Fatalf("parseCheckTarget returned error: %v", err)
	}

	if target.Hostname != "rpc.example.com" {
		t.Fatalf("expected normalized hostname, got %q", target.Hostname)
	}
	if target.Port != "8443" {
		t.Fatalf("expected port 8443, got %q", target.Port)
	}
	if target.Label != "rpc.example.com:8443" {
		t.Fatalf("expected non-default port label, got %q", target.Label)
	}
	if target.URL != "wss://rpc.example.com:8443/ws/v1?foo=bar" {
		t.Fatalf("expected normalized URL, got %q", target.URL)
	}
}

func TestParseUrlForDomainKeepsOnlyMeaningfulPorts(t *testing.T) {
	if got := parseUrlForDomain("https://rpc.example.com:443"); got != "rpc.example.com" {
		t.Fatalf("expected default https port to collapse, got %q", got)
	}
	if got := parseUrlForDomain("https://rpc.example.com:8443"); got != "rpc.example.com:8443" {
		t.Fatalf("expected non-default port to be preserved, got %q", got)
	}
}

func TestParseEthSyncingHandlesFalseNullAndObjects(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		want    bool
		wantErr bool
	}{
		{name: "false", payload: "false", want: false},
		{name: "null", payload: "null", want: false},
		{name: "object", payload: `{"startingBlock":"0x1"}`, want: true},
		{name: "string false", payload: `"false"`, want: false},
		{name: "invalid", payload: `"maybe"`, wantErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseEthSyncing(json.RawMessage(tc.payload))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for payload %s", tc.payload)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for payload %s: %v", tc.payload, err)
			}
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestParseChainIDSupportsHexAndDecimal(t *testing.T) {
	if got, ok := parseChainID("0x2a"); !ok || got != 42 {
		t.Fatalf("expected hex chain id 0x2a to parse as 42, got %d ok=%v", got, ok)
	}
	if got, ok := parseChainID("42"); !ok || got != 42 {
		t.Fatalf("expected decimal chain id 42 to parse as 42, got %d ok=%v", got, ok)
	}
	if _, ok := parseChainID("not-a-chain-id"); ok {
		t.Fatalf("expected invalid chain id to fail parsing")
	}
}
