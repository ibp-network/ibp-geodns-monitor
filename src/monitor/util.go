package monitor

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type CheckTarget struct {
	Scheme   string
	Hostname string
	Port     string
	Label    string
	RequestURI string
	URL      string
}

func (t CheckTarget) DialAddress(ip string) string {
	return net.JoinHostPort(ip, t.Port)
}

func getIntOption(extraOptions map[string]interface{}, key string, defaultValue int) int {
	if extraOptions == nil {
		return defaultValue
	}

	switch val := extraOptions[key].(type) {
	case float64:
		return int(val)
	case float32:
		return int(val)
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case json.Number:
		if parsed, err := val.Int64(); err == nil {
			return int(parsed)
		}
	}

	return defaultValue
}

func getFloatOption(extraOptions map[string]interface{}, key string, defaultValue float64) float64 {
	if extraOptions == nil {
		return defaultValue
	}

	switch val := extraOptions[key].(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		if parsed, err := val.Float64(); err == nil {
			return parsed
		}
	}

	return defaultValue
}

func parseCheckTarget(raw string, defaultScheme string) (CheckTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CheckTarget{}, fmt.Errorf("empty target")
	}

	if !strings.Contains(raw, "://") {
		raw = defaultScheme + "://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return CheckTarget{}, fmt.Errorf("invalid target %q: %w", raw, err)
	}

	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme == "" {
		scheme = strings.ToLower(strings.TrimSpace(defaultScheme))
	}

	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return CheckTarget{}, fmt.Errorf("target %q is missing a host", raw)
	}

	port := strings.TrimSpace(u.Port())
	if port == "" {
		port = defaultPortForScheme(scheme)
	}

	requestURI := u.EscapedPath()
	if requestURI == "" {
		requestURI = "/"
	}
	if u.RawQuery != "" {
		requestURI += "?" + u.RawQuery
	}

	return CheckTarget{
		Scheme:     scheme,
		Hostname:   host,
		Port:       port,
		Label:      normalizedTargetLabel(host, port, scheme),
		RequestURI: requestURI,
		URL:        buildTargetURL(scheme, host, port, requestURI),
	}, nil
}

func parseUrlForDomain(raw string) string {
	target, err := parseCheckTarget(raw, "https")
	if err != nil {
		return ""
	}
	return target.Label
}

func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https", "wss":
		return "443"
	case "http", "ws":
		return "80"
	default:
		return ""
	}
}

func buildTargetURL(scheme, host, port, requestURI string) string {
	authority := strings.ToLower(strings.TrimSpace(host))
	defaultPort := defaultPortForScheme(scheme)
	if port != "" && port != defaultPort {
		authority = net.JoinHostPort(authority, port)
	}
	return scheme + "://" + authority + requestURI
}

func normalizedTargetLabel(host, port, scheme string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	defaultPort := defaultPortForScheme(scheme)
	if port == "" || port == defaultPort {
		return host
	}
	return net.JoinHostPort(host, port)
}

func parseFlexibleInt(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		out, err := v.Int64()
		return out, err == nil
	case string:
		out, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return out, err == nil
	default:
		return 0, false
	}
}
