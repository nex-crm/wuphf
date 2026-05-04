package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCmdDoctorReportsBrokerHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" || r.Method != http.MethodGet {
			http.Error(w, "wrong route", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","session_mode":"office","focus_mode":true,"provider":"codex","provider_model":"gpt-5.4","memory_backend":"markdown","memory_backend_active":"markdown","memory_backend_ready":true,"nex_connected":false}`))
	}))
	defer ts.Close()

	t.Setenv("WUPHF_TEAM_BROKER_URL", ts.URL)

	ctx, out := captureMessages()
	if err := cmdDoctor(ctx, ""); err != nil {
		t.Fatalf("cmdDoctor: %v", err)
	}
	joined := strings.Join(*out, "\n")
	for _, want := range []string{
		"Doctor: ok",
		"Provider: codex · gpt-5.4",
		"Focus mode: true",
		"Memory ready: true",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, joined)
		}
	}
}
