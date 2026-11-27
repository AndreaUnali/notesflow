package agent

import (
	"context"
	"sync"
)

type tokenKey struct{}

// TokenMeter totals usage for one phase. A phase can display a count when all
// of its model calls reported one.
type TokenMeter struct {
	mu      sync.Mutex
	total   int
	known   bool
	missing bool
}

func MeasureTokens(ctx context.Context) (context.Context, *TokenMeter) {
	meter := &TokenMeter{}
	return context.WithValue(ctx, tokenKey{}, meter), meter
}

func ReportTokens(ctx context.Context, count *int) {
	meter, _ := ctx.Value(tokenKey{}).(*TokenMeter)
	if meter == nil {
		return
	}
	meter.mu.Lock()
	defer meter.mu.Unlock()
	if count == nil || *count < 0 {
		meter.missing = true
		return
	}
	meter.known = true
	meter.total += *count
}

func (m *TokenMeter) Take() *int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total, available := m.total, m.known && !m.missing
	m.total, m.known, m.missing = 0, false, false
	if available {
		return &total
	}
	return nil
}
