package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestObserveAndScrape(t *testing.T) {
	m := New("v-test")

	m.ObserveScan("nightly", "consumer-stale", 3, 2*time.Second, nil)
	m.ObserveScan("nightly", "consumer-stale", 0, time.Second, errors.New("boom"))
	m.ObserveApply(5, 1, 2, 0, 1, 10*time.Minute)

	body := scrape(t, m)

	// Prometheus orders labels alphabetically in the text exposition format.
	want := []string{
		`natsie_build_info{version="v-test"} 1`,
		`natsie_scan_candidates{kind="consumer-stale",schedule="nightly"} 3`,
		`natsie_scan_runs_total{kind="consumer-stale",outcome="success",schedule="nightly"} 1`,
		`natsie_scan_runs_total{kind="consumer-stale",outcome="error",schedule="nightly"} 1`,
		`natsie_apply_consumers_total{outcome="deleted"} 5`,
		`natsie_apply_consumers_total{outcome="failed"} 1`,
		`natsie_approval_latency_seconds_count 1`,
	}

	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("scrape missing %q\n--- body ---\n%s", w, body)
		}
	}
}

func TestErroredScanLeavesCandidatesGaugeUntouched(t *testing.T) {
	m := New("v-test")

	// Only an errored run for this schedule — the candidates gauge should not
	// be reported (no misleading zero), only the run counter.
	m.ObserveScan("flaky", "consumer-stale", 0, time.Second, errors.New("boom"))

	body := scrape(t, m)
	if strings.Contains(body, `natsie_scan_candidates{kind="consumer-stale",schedule="flaky"}`) {
		t.Errorf("errored scan should not emit a candidates gauge\n%s", body)
	}

	if !strings.Contains(body, `natsie_scan_runs_total{kind="consumer-stale",outcome="error",schedule="flaky"} 1`) {
		t.Errorf("missing error run counter\n%s", body)
	}
}

func TestNilReceiverIsSafe(t *testing.T) {
	var m *Metrics

	// Neither call should panic — callers record unconditionally.
	m.ObserveScan("x", "y", 1, time.Second, nil)
	m.ObserveApply(1, 1, 1, 1, 1, time.Minute)
}

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}

	return rec.Body.String()
}
