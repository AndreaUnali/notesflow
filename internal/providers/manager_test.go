package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreaUnali/notesflow/internal/agent"
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

func TestDelegationSuspendsBackgroundInference(t *testing.T) {
	profile := config.Profile{ID: "model", Driver: "openai-compatible", Model: "example"}
	manager, err := Open(filepath.Join(t.TempDir(), "providers.json"), config.Providers{
		Mode: "pool", Profiles: []config.Profile{profile},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, endInference, err := manager.beginInference(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer endInference()
	endDelegation := manager.BeginDelegation()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("an in-flight background request should be interrupted")
	}
	if err := manager.finish(ctx, profile, errors.New("delayed background failure")); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted inference returned %v", err)
	}
	if got := manager.Snapshot().Profiles[0]; got.Status != "pronto" || !got.AvailableAt.IsZero() {
		t.Fatalf("interrupted background work put the provider in cooldown: %+v", got)
	}
	waitCtx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := manager.beginInference(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("inference did not wait for the delegation: %v", err)
	}
	endDelegation()
	_, endNext, err := manager.beginInference(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	endNext()
}

func TestGuideWaitsForShortProviderCooldown(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"Risposta\"}"}}]}`))
	}))
	defer server.Close()
	manager, err := Open(filepath.Join(t.TempDir(), "providers.json"), config.Providers{
		Mode: "pool", Profiles: []config.Profile{
			{ID: "model", Driver: "openai-compatible", Model: "example", BaseURL: server.URL},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.state.Usage["model"] = usage{RetryAt: time.Now().Add(60 * time.Millisecond)}
	result, err := manager.Guide(t.Context(), agent.Request{Goal: "Prova"})
	if err != nil || result.Summary != "Risposta" || attempts.Load() != 1 {
		t.Fatalf("guide returned %+v, error %v, attempts %d", result, err, attempts.Load())
	}
}

func TestDirectGuideRetriesTransientServiceFailure(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"Pronta\"}"}}]}`))
	}))
	defer server.Close()
	manager, err := Open(filepath.Join(t.TempDir(), "providers.json"), config.Providers{
		Mode: "byok", Active: "api", Profiles: []config.Profile{
			{ID: "api", Driver: "openai-compatible", Model: "example", BaseURL: server.URL},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	draft, err := manager.Guide(ctx, agent.Request{Goal: "Prova"})
	if err != nil || draft.Summary != "Pronta" || attempts.Load() != 2 || time.Since(start) < time.Second {
		t.Fatalf("guide returned %+v, error %v, attempts %d", draft, err, attempts.Load())
	}
}
