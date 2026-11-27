package watcher

import (
	"context"
	"errors"
	"time"

	"github.com/AndreaUnali/notesflow/internal/agent"
	"github.com/AndreaUnali/notesflow/internal/graph"
	"github.com/AndreaUnali/notesflow/internal/providers"
)

type Analyzer interface {
	Infer(context.Context, graph.Delta) ([]graph.Edge, error)
}

type AnalysisActivity struct {
	At              time.Time
	Total, Resolved int
	Finished        bool
	Err             error
	Tokens          *int
}

// Analyze drains persistent pending hashes independently of the filesystem watcher.
func Analyze(ctx context.Context, store *graph.Store, analyzer Analyzer, candidates, deltaRunes int, report func(AnalysisActivity)) {
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
		analysisCtx, meter := agent.MeasureTokens(ctx)
		progress := AnalysisActivity{At: time.Now(), Total: len(pending)}
		if report != nil {
			report(progress)
		}
		var retry time.Duration
		for _, id := range pending {
			if ctx.Err() != nil {
				return
			}
			delta, exists := store.Delta(id, candidates, deltaRunes)
			if !exists {
				continue
			}
			edges, err := analyzer.Infer(analysisCtx, delta)
			if err == nil {
				err = store.Resolve(delta, edges)
			}
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				progress.Resolved++
				store.Log("Delta indicizzato · " + id)
				continue
			}
			progress.Err = err
			if !errors.Is(err, context.Canceled) {
				store.Log("Delta in attesa · " + err.Error())
				retry = 30 * time.Second
				var unavailable *providers.Unavailable
				if errors.As(err, &unavailable) && !unavailable.RetryAt.IsZero() {
					retry = max(time.Second, time.Until(unavailable.RetryAt))
				}
			}
			break
		}
		if report != nil {
			progress.At, progress.Finished, progress.Tokens = time.Now(), true, meter.Take()
			report(progress)
		}
		if retry > 0 {
			timer := time.NewTimer(retry)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}
