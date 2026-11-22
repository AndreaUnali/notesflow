package providers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/parser"
)

func TestPoolFallsBackAndPersistsQuotaWindow(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("endpoint: %q", r.URL.Path)
		}
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "providers.json")
	cfg := config.Providers{
		Mode: "pool",
		Profiles: []config.Profile{
			{ID: "remoto", Driver: "openai-compatible", Model: "prova", BaseURL: server.URL + "/v1", RequestLimit: 2, Window: "1h"},
			{ID: "locale", Driver: "mock", Model: "riassunto"},
		},
	}
	manager, err := Open(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tasks := []parser.Task{{Kind: "VISUAL", File: "esempio.tex", Text: "Schema", Section: "Titolo", Line: 7}}
	text, err := manager.Summarize(t.Context(), tasks)
	if err != nil || text == "" {
		t.Fatalf("fallback: %q, %v", text, err)
	}
	manager, err = Open(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	status := manager.Snapshot()
	if status.Active != "locale" || len(status.History) != 1 ||
		status.Profiles[0].Remaining != 1 || status.Profiles[0].AvailableAt.IsZero() {
		t.Fatalf("stato provider: %+v", status)
	}
	if _, err := manager.Summarize(t.Context(), tasks); err != nil {
		t.Fatal(err)
	}
	if total := attempts.Load(); total != 1 {
		t.Fatalf("tentativi HTTP: %d, atteso 1", total)
	}
}
