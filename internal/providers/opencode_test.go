package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeErrorIncludesActionWithoutRawOutput(t *testing.T) {
	tests := []struct{ name, output, cause, action string }{
		{
			"router quota",
			"printf '%s\\n' '{\"type\":\"error\",\"error\":{\"name\":\"APIError\",\"data\":{\"statusCode\":429,\"message\":\"opaque-token\"}}}'",
			"HTTP 429", "quota",
		},
		{
			"missing model on stderr",
			"printf '%s\\n' 'ModelNotFoundError opaque-token' >&2; exit 1",
			"modello o endpoint non trovato", "opencode models",
		},
		{
			"router outage",
			"printf '%s\\n' '{\"type\":\"error\",\"error\":{\"message\":\"No deployments available via opaque-token\"}}'",
			"indisponibili", "modelli a monte",
		},
		{
			"empty response",
			"exit 0",
			"senza restituire testo", "router locale",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeOpenCode(t, tc.output)
			_, err := openCode(t.Context(), "", "a test prompt")
			if err == nil {
				t.Fatal("expected an error")
			}
			cause, action := Explain(err)
			if !strings.Contains(cause, tc.cause) || !strings.Contains(action, tc.action) ||
				strings.Contains(cause+action, "opaque-token") {
				t.Fatalf("cause %q, action %q", cause, action)
			}
		})
	}
}

func TestOpenCodeReturnsTextAndRespondsToAnEarlierDeadline(t *testing.T) {
	fakeOpenCode(t, "printf '%s\\n' '{\"type\":\"text\",\"part\":{\"text\":\"OK\"}}'")
	if result, err := openCode(t.Context(), "", "test"); err != nil || result != "OK" {
		t.Fatalf("result %q, error %v", result, err)
	}
	fakeOpenCode(t, "exec /bin/sleep 3")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := openCode(ctx, "", "test"); err == nil || !strings.Contains(err.Error(), "limite") {
		t.Fatalf("expected a diagnostic timeout, got %v", err)
	}
}

func fakeOpenCode(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
