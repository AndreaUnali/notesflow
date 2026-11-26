package editor

import (
	"context"
	"errors"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/network"
	"github.com/neovim/go-client/nvim"
)

const findBuffer = `local path = ...
local buffer = 0
for _, candidate in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(candidate) and vim.api.nvim_buf_get_name(candidate) == path then
    buffer = candidate
    break
  end
end
`

func (e *Editor) Position(ctx context.Context) (network.Position, error) {
	var pos network.Position
	err := e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(`local view = vim.fn.winsaveview()
return {File = vim.api.nvim_buf_get_name(0), Line = view.lnum, Column = view.col, Top = view.topline}`, &pos)
	})
	return pos, err
}

type ExitState struct {
	Unsaved  bool
	Position network.Position
}

func (e *Editor) ExitState(ctx context.Context) (ExitState, error) {
	var state ExitState
	err := e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(`local view = vim.fn.winsaveview()
return {
  Unsaved = #vim.fn.getbufinfo({bufmodified = 1}) > 0,
  Position = {File = vim.api.nvim_buf_get_name(0), Line = view.lnum, Column = view.col, Top = view.topline}
}`, &state)
	})
	return state, err
}

func (e *Editor) SaveAll(ctx context.Context) error {
	var message string
	err := e.request(ctx, func(v *nvim.Nvim) error {
		mode, err := v.Mode()
		if err != nil {
			return err
		}
		if mode.Blocking {
			if _, err := v.Input("<Esc>"); err != nil {
				return err
			}
		}
		return v.ExecLua(`local ok, message = pcall(vim.cmd, 'silent wall')
if not ok then return tostring(message) end
if #vim.fn.getbufinfo({bufmodified = 1}) > 0 then
  return 'Restano file modificati in Neovim.'
end
return ''`, &message)
	})
	if err == nil && message != "" {
		return errors.New(message)
	}
	return err
}

func (e *Editor) OpenAt(ctx context.Context, file string, pos network.Position) error {
	return e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(`local path, line, column, top = ...
vim.cmd('hide edit ' .. vim.fn.fnameescape(path))
line = math.min(line, vim.api.nvim_buf_line_count(0))
local text = vim.api.nvim_buf_get_lines(0, line - 1, line, true)[1]
local col = math.min(column, #(text or ''))
vim.api.nvim_win_set_cursor(0, {line, col})
if top > 0 then
  local view = vim.fn.winsaveview()
  view.topline = math.min(top, line)
  vim.fn.winrestview(view)
end`, nil, file, max(1, pos.Line), max(0, pos.Column), max(0, pos.Top))
	})
}

// Modified reports whether an open buffer has edits that have not yet reached disk.
func (e *Editor) Modified(ctx context.Context, path string) (bool, error) {
	var modified bool
	err := e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(findBuffer+`return buffer > 0 and vim.bo[buffer].modified`, &modified, path)
	})
	return modified, err
}

// Refresh rereads an externally changed buffer only when Neovim can do so safely.
func (e *Editor) Refresh(ctx context.Context, path string) error {
	return e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(findBuffer+`if buffer > 0 and not vim.bo[buffer].modified then
  vim.cmd('silent checktime ' .. buffer)
end`, nil, path)
	})
}

type Buffer struct {
	ID        int
	Changed   int
	Modified  bool
	EndOfLine bool
	Lines     []string
}

func (b Buffer) Text() string {
	text := strings.Join(b.Lines, "\n")
	if b.EndOfLine {
		text += "\n"
	}
	return text
}

func (e *Editor) Buffer(ctx context.Context, path string) (Buffer, error) {
	var buffer Buffer
	err := e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(findBuffer+`if buffer == 0 then return {ID = 0} end
return {
  ID = buffer, Changed = vim.api.nvim_buf_get_changedtick(buffer), Modified = vim.bo[buffer].modified,
  EndOfLine = vim.bo[buffer].endofline, Lines = vim.api.nvim_buf_get_lines(buffer, 0, -1, true)
}`, &buffer, path)
	})
	return buffer, err
}

func (e *Editor) ReplaceBuffer(ctx context.Context, path string, before Buffer, next string) (applied, saved bool, err error) {
	lines := strings.Split(strings.TrimSuffix(next, "\n"), "\n")
	first := 0
	for first < min(len(before.Lines), len(lines)) && before.Lines[first] == lines[first] {
		first++
	}
	last, updatedLast := len(before.Lines), len(lines)
	for last > first && updatedLast > first && before.Lines[last-1] == lines[updatedLast-1] {
		last--
		updatedLast--
	}
	var status int
	err = e.request(ctx, func(v *nvim.Nvim) error {
		return v.ExecLua(`local buf, tick, first, last, lines, path, expected, was_modified = ...
if not vim.api.nvim_buf_is_loaded(buf) or vim.api.nvim_buf_get_changedtick(buf) ~= tick then
  return 0
end
if not was_modified then
  local file = io.open(path, 'rb')
  if file == nil then return 0 end
  local disk = file:read('*a')
  file:close()
  if disk:gsub('\r\n', '\n') ~= expected or vim.bo[buf].modified then return 0 end
end
vim.api.nvim_buf_set_lines(buf, first, last, true, lines)
if not was_modified then
  local wrote = pcall(vim.api.nvim_buf_call, buf, function()
    vim.cmd('silent write')
  end)
  if wrote and not vim.bo[buf].modified then return 2 end
end
return 1`, &status, before.ID, before.Changed, first, last, lines[first:updatedLast], path, before.Text(), before.Modified)
	})
	return status > 0, status == 2, err
}
