package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CheckTeXEdit verifies a candidate in an independent working copy.
func CheckTeXEdit(ctx context.Context, root, main, file, engine, replacement string) error {
	source, err := WorkspaceFile(root, file)
	if err != nil {
		return err
	}
	target, err := filepath.Rel(root, source)
	if err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "notesflow-todo-ai-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	stage, err := filepath.EvalSymlinks(temp)
	if err != nil {
		return err
	}
	files, _, err := readImportFiles(ctx, root, "")
	if err != nil {
		return err
	}
	for _, relative := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.FromSlash(relative)
		if name == target {
			continue
		}
		dest := filepath.Join(stage, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := copyImportFile(filepath.Join(root, name), dest); err != nil {
			return err
		}
	}
	edited := filepath.Join(stage, target)
	if err := os.MkdirAll(filepath.Dir(edited), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(edited, []byte(replacement), 0644); err != nil {
		return err
	}
	build := filepath.Join(".notesflow", "build")
	out, err := CompileIn(ctx, stage, main, engine, build)
	if err != nil {
		return err
	}
	if filepath.Clean(filepath.FromSlash(main)) == target {
		return nil
	}
	stem := strings.TrimSuffix(filepath.Base(out), ".pdf")
	trace, err := os.ReadFile(filepath.Join(stage, build, stem+".fls"))
	if err != nil {
		return errors.New("il compilatore non ha riportato i file inclusi nel PDF")
	}
	for _, line := range strings.Split(string(trace), "\n") {
		name, ok := strings.CutPrefix(line, "INPUT ")
		if !ok {
			continue
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(filepath.Dir(out), name)
		}
		if filepath.Clean(name) == edited {
			return nil
		}
	}
	return errors.New("il documento principale non include il sorgente del TODO-AI")
}
