// Package editor embeds Neovim's RPC UI in a terminal panel.
package editor

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/neovim/go-client/nvim"

	"github.com/AndreaUnali/notesflow/internal/palette"
)

//go:embed theme.lua
var theme string

type Editor struct {
	client  *nvim.Nvim
	frames  chan Frame
	done    chan struct{}
	calls   chan func(*nvim.Nvim)
	cleanup sync.Once
}

func Start(ctx context.Context, dir string, width, height int) (*Editor, error) {
	v, err := nvim.NewChildProcess(
		nvim.ChildProcessContext(ctx), nvim.ChildProcessDir(dir),
		nvim.ChildProcessArgs("--embed", "--cmd", "let g:notesflow = v:true"), nvim.ChildProcessServe(false),
		nvim.ChildProcessLogf(func(string, ...any) {}),
	)
	if err != nil {
		return nil, err
	}
	e := &Editor{client: v, frames: make(chan Frame, 1), done: make(chan struct{}), calls: make(chan func(*nvim.Nvim), 256)}
	var screen grid
	err = v.RegisterHandler("redraw", func(updates ...[]any) {
		if frame, ok := screen.update(updates); ok {
			select {
			case <-e.frames:
			default:
			}
			e.frames <- frame
		}
	})
	go func() {
		_ = v.Serve()
		close(e.done)
		e.close()
	}()
	if err != nil {
		e.close()
		return nil, err
	}
	if err := v.AttachUI(width, height, map[string]any{"ext_linegrid": true, "rgb": true}); err != nil {
		e.close()
		return nil, err
	}
	if os.Getenv("NOTESFLOW_NVIM_THEME") != "native" {
		_ = v.ExecLua(theme, nil, map[string]string{
			"background": palette.Background, "surface": palette.Surface, "line": palette.Line,
			"text": palette.Text, "muted": palette.Muted, "cyan": palette.Cyan, "pink": palette.Pink,
			"purple": palette.Purple, "yellow": palette.Yellow,
		})
	}
	go e.dispatch()
	return e, nil
}

func (e *Editor) Frames() <-chan Frame  { return e.frames }
func (e *Editor) Done() <-chan struct{} { return e.done }
func (e *Editor) close()                { e.cleanup.Do(func() { _ = e.client.Close() }) }

func (e *Editor) dispatch() {
	for {
		select {
		case <-e.done:
			return
		case call := <-e.calls:
			call(e.client)
		}
	}
}

func (e *Editor) queue(call func(*nvim.Nvim)) bool {
	select {
	case <-e.done:
		return false
	default:
	}
	select {
	case e.calls <- call:
		return true
	default:
		return false
	}
}

func (e *Editor) Resize(width, height int) {
	e.queue(func(v *nvim.Nvim) { _ = v.TryResizeUI(width, height) })
}

func (e *Editor) Open(ctx context.Context, file string, line int) error {
	return e.request(ctx, func(v *nvim.Nvim) error {
		// Keep previous buffers, even if they contain unsaved changes.
		return v.ExecLua(`local path, line = ...
vim.cmd('hide edit ' .. vim.fn.fnameescape(path))
vim.api.nvim_win_set_cursor(0, {math.min(line, vim.api.nvim_buf_line_count(0)), 0})`, nil, file, max(1, line))
	})
}

func (e *Editor) Agent(ctx context.Context, dir string) error {
	if _, err := exec.LookPath("opencode"); err != nil {
		return err
	}
	return e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(`local dir = ...
vim.cmd('hide enew')
assert(vim.fn.termopen({'opencode'}, {cwd=dir}) > 0, 'OpenCode non avviato')
vim.cmd.startinsert()`, nil, dir)
	})
}

func (e *Editor) Unsaved(ctx context.Context) (bool, error) {
	var modified bool
	err := e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(`return #vim.fn.getbufinfo({bufmodified = 1}) > 0`, &modified)
	})
	if err != nil {
		return false, err
	}
	return modified, nil
}

func (e *Editor) request(ctx context.Context, call func(*nvim.Nvim) error) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
	}
	result := make(chan error, 1)
	if !e.queue(func(v *nvim.Nvim) {
		if err := ctx.Err(); err != nil {
			result <- err
			return
		}
		result <- call(v)
	}) {
		return errors.New("Neovim non disponibile")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.done:
		return os.ErrClosed
	case err := <-result:
		return err
	}
}

func (e *Editor) Key(key tea.KeyMsg) bool {
	if key.Paste {
		return e.queue(func(v *nvim.Nvim) { _, _ = v.Paste(string(key.Runes), true, -1) })
	}
	input := keyNotation(key)
	if input == "" {
		return true
	}
	return e.queue(func(v *nvim.Nvim) {
		_, _ = v.Input(input)
	})
}

func (e *Editor) Mouse(button, action, modifiers string, row, col int) {
	e.queue(func(v *nvim.Nvim) { _ = v.InputMouse(button, action, modifiers, 1, row, col) })
}

func keyNotation(key tea.KeyMsg) string {
	s := key.String()
	if key.Type == tea.KeySpace {
		if key.Alt {
			return "<M-Space>"
		}
		return " "
	}
	if key.Type == tea.KeyRunes {
		prefix := ""
		if key.Alt {
			prefix = "M-"
		}
		if prefix != "" && len(key.Runes) == 1 {
			value := string(key.Runes)
			if value == "<" {
				value = "LT"
			}
			return "<" + prefix + value + ">"
		}
		return strings.ReplaceAll(string(key.Runes), "<", "<LT>")
	}
	prefix := ""
	s = strings.TrimPrefix(s, "alt+")
	if key.Alt {
		prefix += "M-"
	}
	for _, modifier := range []struct{ name, code string }{{"ctrl+", "C-"}, {"shift+", "S-"}} {
		if strings.HasPrefix(s, modifier.name) {
			prefix += modifier.code
			s = strings.TrimPrefix(s, modifier.name)
		}
	}
	names := map[string]string{
		"enter": "CR", "esc": "Esc", "tab": "Tab", "backspace": "BS",
		"delete": "Del", "insert": "Insert", "up": "Up", "down": "Down",
		"left": "Left", "right": "Right", "home": "Home", "end": "End",
		"pgup": "PageUp", "pgdown": "PageDown", " ": "Space",
	}
	if name, ok := names[s]; ok {
		return "<" + prefix + name + ">"
	}
	if len(s) == 1 && prefix != "" {
		return "<" + prefix + s + ">"
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(s, "f")); strings.HasPrefix(s, "f") && err == nil && n > 0 && n <= 37 {
		return fmt.Sprintf("<%sF%d>", prefix, n)
	}
	return ""
}
