package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cfg "github.com/ibp-network/ibp-geodns-libs/config"
	dat "github.com/ibp-network/ibp-geodns-libs/data"
)

func TestSelectResultsForAPIPrefersOfficialResults(t *testing.T) {
	resetResultGettersForTest(t)

	expected := sampleSiteResults("ping")
	getOfficialResults = func() ([]dat.SiteResult, []dat.DomainResult, []dat.EndpointResult) {
		return expected, nil, nil
	}
	getLocalResults = func() ([]dat.SiteResult, []dat.DomainResult, []dat.EndpointResult) {
		return sampleSiteResults("local-ping"), nil, nil
	}

	sites, domains, endpoints, source := selectResultsForAPI()
	if source != "official" {
		t.Fatalf("expected official source, got %q", source)
	}
	if len(domains) != 0 || len(endpoints) != 0 {
		t.Fatalf("expected only site results in test selection")
	}
	if len(sites) != 1 || sites[0].Check.Name != "ping" {
		t.Fatalf("expected official site result to be selected, got %#v", sites)
	}
}

func TestSelectResultsForAPIFallsBackToLocalResults(t *testing.T) {
	resetResultGettersForTest(t)

	getOfficialResults = func() ([]dat.SiteResult, []dat.DomainResult, []dat.EndpointResult) {
		return nil, nil, nil
	}
	getLocalResults = func() ([]dat.SiteResult, []dat.DomainResult, []dat.EndpointResult) {
		return sampleSiteResults("ping"), nil, nil
	}

	sites, _, _, source := selectResultsForAPI()
	if source != "local" {
		t.Fatalf("expected local fallback source, got %q", source)
	}
	if len(sites) != 1 || sites[0].Check.Name != "ping" {
		t.Fatalf("expected local site result to be selected, got %#v", sites)
	}
}

func TestHandleResultsSetsSourceHeaderAndBodyFromFallback(t *testing.T) {
	resetResultGettersForTest(t)

	getOfficialResults = func() ([]dat.SiteResult, []dat.DomainResult, []dat.EndpointResult) {
		return nil, nil, nil
	}
	getLocalResults = func() ([]dat.SiteResult, []dat.DomainResult, []dat.EndpointResult) {
		return sampleSiteResults("ping"), nil, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/results", nil)
	rec := httptest.NewRecorder()

	handleResults(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-IBP-Results-Source"); got != "local" {
		t.Fatalf("expected local source header, got %q", got)
	}

	var payload struct {
		SiteResults []struct {
			CheckName string `json:"CheckName"`
		} `json:"SiteResults"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(payload.SiteResults) != 1 || payload.SiteResults[0].CheckName != "ping" {
		t.Fatalf("expected fallback site payload, got %s", rec.Body.String())
	}
}

func resetResultGettersForTest(t *testing.T) {
	t.Helper()
	getOfficialResults = dat.GetOfficialResults
	getLocalResults = dat.GetLocalResults
	t.Cleanup(func() {
		getOfficialResults = dat.GetOfficialResults
		getLocalResults = dat.GetLocalResults
	})
}

func sampleSiteResults(checkName string) []dat.SiteResult {
	return []dat.SiteResult{
		{
			Check:  cfg.Check{Name: checkName},
			IsIPv6: false,
			Results: []dat.Result{
				{
					Status:    true,
					Checktime: time.Unix(1700000000, 0).UTC(),
				},
			},
		},
	}
}
