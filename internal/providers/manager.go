// Package providers manages inference backends, quota estimates and model rotation.
package providers

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"sync"
	"time"

	"github.com/AndreaUnali/notesflow/internal/config"
	"github.com/AndreaUnali/notesflow/internal/storage"
)

type usage struct {
	Started time.Time
	Used    int
	RetryAt time.Time
}

type Change struct {
	Time    time.Time
	Profile string
	Model   string
}

type persisted struct {
	Active  string
	Model   string
	History []Change
	Usage   map[string]usage
}

type ProfileStatus struct {
	ID, Driver, Model string
	Status            string
	Remaining         int
	WindowEnd         time.Time
	AvailableAt       time.Time
}

type Status struct {
	Mode, Active string
	Profiles     []ProfileStatus
	History      []Change
}

type Manager struct {
	path     string
	mode     string
	profiles []config.Profile
	mu       sync.Mutex
	state    persisted
}

type requestError struct {
	message string
	retryAt time.Time
	status  int
}

func (e *requestError) Error() string { return e.message }

type Unavailable struct {
	RetryAt time.Time
	cause   error
}

func (e *Unavailable) Unwrap() error { return e.cause }

func (e *Unavailable) Error() string {
	if e.cause != nil {
		return "provider indisponibili: " + e.cause.Error()
	}
	return "nessun profilo disponibile"
}

func Open(path string, cfg config.Providers) (*Manager, error) {
	m := &Manager{path: path, mode: cfg.Mode, state: persisted{Usage: map[string]usage{}}}
	for _, profile := range cfg.Profiles {
		if cfg.Mode == "pool" || profile.ID == cfg.Active {
			m.profiles = append(m.profiles, profile)
		}
	}
	if err := storage.Load(path, &m.state); err != nil {
		return nil, fmt.Errorf("stato provider: %w", err)
	}
	if m.state.Usage == nil {
		m.state.Usage = map[string]usage{}
	}
	return m, nil
}

func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{Mode: m.mode, History: slices.Clone(m.state.History)}
	for _, p := range m.profiles {
		info := m.status(p, time.Now())
		status.Profiles = append(status.Profiles, info)
		if p.ID == m.state.Active && p.Model == m.state.Model {
			status.Active = p.ID
		}
	}
	return status
}

func (m *Manager) status(profile config.Profile, now time.Time) ProfileStatus {
	u := m.state.Usage[profile.ID]
	info := ProfileStatus{ID: profile.ID, Driver: profile.Driver, Model: profile.Model, Status: "pronto", Remaining: -1}
	if profile.RequestLimit > 0 {
		info.Remaining = profile.RequestLimit
		end := u.Started.Add(profile.Duration())
		if now.Before(end) {
			info.WindowEnd = end
			info.Remaining = max(0, profile.RequestLimit-u.Used)
		}
		if info.Remaining == 0 {
			info.AvailableAt, info.Status = end, "quota esaurita"
		}
	}
	if now.Before(u.RetryAt) && u.RetryAt.After(info.AvailableAt) {
		info.AvailableAt, info.Status = u.RetryAt, "in pausa"
	}
	switch {
	case profile.Driver == "openai-compatible" && profile.APIKeyEnv != "" && os.Getenv(profile.APIKeyEnv) == "":
		info.Status = "chiave assente"
		info.AvailableAt = time.Time{}
	case profile.Driver == "opencode":
		if _, err := exec.LookPath("opencode"); err != nil {
			info.Status = "OpenCode assente"
			info.AvailableAt = time.Time{}
		}
	}
	return info
}

// run reserves quota before I/O, then falls back to each remaining pool profile.
func (m *Manager) run(ctx context.Context, do func(config.Profile) error) (string, error) {
	return m.runFor(ctx, nil, do)
}

func (m *Manager) runFor(ctx context.Context, eligible func(config.Profile) bool, do func(config.Profile) error) (string, error) {
	m.mu.Lock()
	start := slices.IndexFunc(m.profiles, func(p config.Profile) bool { return p.ID == m.state.Active })
	m.mu.Unlock()
	start++
	var cause error
	for offset := range m.profiles {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		profile := m.profiles[(start+offset)%len(m.profiles)]
		if eligible != nil && !eligible(profile) {
			continue
		}
		ok, err := m.reserve(profile)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		err = do(profile)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if saveErr := m.finish(profile, err); saveErr != nil {
			return "", saveErr
		}
		if err == nil {
			return profile.Driver, nil
		}
		cause = fmt.Errorf("%s: %w", profile.ID, err)
	}
	var retry time.Time
	for _, info := range m.Snapshot().Profiles {
		if !info.AvailableAt.IsZero() && (retry.IsZero() || info.AvailableAt.Before(retry)) {
			retry = info.AvailableAt
		}
	}
	return "", &Unavailable{RetryAt: retry, cause: cause}
}

func (m *Manager) reserve(profile config.Profile) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.status(profile, now).Status != "pronto" {
		return false, nil
	}
	next := m.state
	next.Usage = maps.Clone(next.Usage)
	u := next.Usage[profile.ID]
	if u.Started.IsZero() || (profile.RequestLimit > 0 && !now.Before(u.Started.Add(profile.Duration()))) {
		u.Started, u.Used = now, 0
	}
	u.Used++
	next.Usage[profile.ID] = u
	return true, m.commit(next)
}

func (m *Manager) finish(profile config.Profile, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.state
	now := time.Now()
	switch {
	case err != nil:
		next.Usage = maps.Clone(next.Usage)
		u := next.Usage[profile.ID]
		retry := now.Add(30 * time.Second)
		var failure *requestError
		if errors.As(err, &failure) && failure.retryAt.After(now) {
			retry = failure.retryAt
		}
		if retry.After(u.RetryAt) {
			u.RetryAt = retry
		}
		next.Usage[profile.ID] = u
	case next.Active != profile.ID || next.Model != profile.Model:
		next.Active, next.Model = profile.ID, profile.Model
		next.History = append(slices.Clone(next.History), Change{Time: now, Profile: profile.ID, Model: profile.Model})
		if len(next.History) > 200 {
			next.History = next.History[len(next.History)-200:]
		}
	default:
		return nil
	}
	return m.commit(next)
}

func (m *Manager) commit(next persisted) error {
	if err := storage.Save(m.path, next); err != nil {
		return fmt.Errorf("salvataggio provider: %w", err)
	}
	m.state = next
	return nil
}
