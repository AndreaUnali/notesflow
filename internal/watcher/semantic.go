package watcher

import (
	"context"
	"errors"
	"time"

	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/providers"
)

type Analyzer interface {
	Infer(context.Context, graph.Delta) ([]graph.Edge, error)
}

// Analyze drains persistent pending hashes independently of the filesystem watcher.
func Analyze(ctx context.Context, store *graph.Store, analyzer Analyzer, candidates, deltaRunes int) {
	for ctx.Err() == nil {
		_, changed := store.Snapshot()
		pending := store.Pending()
		if len(pending) == 0 {
			select {
			case <-ctx.Done():
			case <-changed:
			}
			continue
		}
		for _, id := range pending {
			if ctx.Err() != nil {
				return
			}
			delta, exists := store.Delta(id, candidates, deltaRunes)
			if !exists {
				continue
			}
			edges, err := analyzer.Infer(ctx, delta)
			if err == nil {
				err = store.Resolve(delta, edges)
			}
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				store.Log("Delta indicizzato · " + id)
				continue
			}
			store.Log("Delta in attesa · " + err.Error())
			delay := 30 * time.Second
			var unavailable *providers.Unavailable
			if errors.As(err, &unavailable) && !unavailable.RetryAt.IsZero() {
				delay = max(time.Second, time.Until(unavailable.RetryAt))
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			break
		}
	}
}
