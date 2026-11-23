package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRedrawCombinesWideCellsAndScrollRegion(t *testing.T) {
	var grid grid
	frame, ok := grid.update([][]any{
		{"grid_resize", []any{1, 6, 3}},
		{"grid_line", []any{1, 1, 0, []any{
			[]any{"語", 0}, []any{""}, []any{"!", 0}, []any{" ", 0, 3},
		}}},
		{"grid_scroll", []any{1, 0, 3, 0, 6, 1, 0}},
		{"mode_change", []any{"insert", 0}},
		{"flush", []any{}},
	})
	if !ok || frame.Mode() != "insert" {
		t.Fatalf("frame: %t, %s", ok, frame.Mode())
	}
	rows := frame.Rows()
	if first := strings.TrimRight(ansi.Strip(rows[0]), " "); first != "語!" {
		t.Fatalf("first row: %q", first)
	}
	if second := strings.TrimSpace(ansi.Strip(rows[1])); second != "" {
		t.Fatalf("second row: %q", second)
	}
	grid.event("grid_clear", []any{1})
	if first := strings.TrimRight(ansi.Strip(frame.Rows()[0]), " "); first != "語!" {
		t.Fatalf("the published frame changed: %q", first)
	}
}
