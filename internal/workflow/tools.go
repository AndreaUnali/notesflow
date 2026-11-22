// Package workflow integrates external tools without invoking a shell.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func WorkspaceFile(root, file string) (string, error) {
	if filepath.IsAbs(file) {
		return "", errors.New("usa un percorso relativo al workspace")
	}
	absolute, err := filepath.EvalSymlinks(filepath.Join(root, file))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("file esterno al workspace")
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("atteso un file regolare")
	}
	return absolute, nil
}

func Edit(ctx context.Context, root, file string, line int) (*exec.Cmd, error) {
	path, err := WorkspaceFile(root, file)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, "nvim", "+"+strconv.Itoa(max(1, line)), "--", path), nil
}

func RemoteEdit(ctx context.Context, root, socket, file string, line int) error {
	if socket == "" {
		return errors.New("configura tools.nvim_server o avvia Notesflow da Neovim")
	}
	path, err := WorkspaceFile(root, file)
	if err != nil {
		return err
	}
	if strings.ContainsAny(path, "\r\n") {
		return errors.New("il percorso contiene un ritorno a capo")
	}
	quoted, err := json.Marshal(path)
	if err != nil {
		return err
	}
	expression := fmt.Sprintf("execute('edit ' . fnameescape(%s)) . cursor(%d, 1)", quoted, max(1, line))
	if err := exec.CommandContext(ctx, "nvim", "--server", socket, "--remote-expr", expression).Run(); err != nil {
		return fmt.Errorf("collegamento a Neovim fallito: %w", err)
	}
	return nil
}

func Open(ctx context.Context, target, app string) error {
	switch runtime.GOOS {
	case "darwin":
		if app != "" {
			return exec.CommandContext(ctx, "open", "-a", app, target).Run()
		}
		return exec.CommandContext(ctx, "open", target).Run()
	case "windows":
		return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target).Run()
	default:
		return exec.CommandContext(ctx, "xdg-open", target).Run()
	}
}

func Compile(ctx context.Context, root, file, engine string) (string, error) {
	absolute, err := WorkspaceFile(root, file)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(absolute), ".tex") {
		return "", errors.New("seleziona un file .tex")
	}
	input := "." + string(os.PathSeparator) + filepath.Base(absolute)
	var args []string
	switch engine {
	case "latexmk":
		args = []string{"-pdf", "-interaction=nonstopmode", "-halt-on-error", "-synctex=1", input}
	case "pdflatex", "xelatex", "lualatex":
		args = []string{"-interaction=nonstopmode", "-halt-on-error", "-synctex=1", input}
	case "tectonic":
		args = []string{"--synctex", input}
	default:
		return "", errors.New("motore LaTeX non supportato")
	}
	cmd := exec.CommandContext(ctx, engine, args...)
	cmd.Dir = filepath.Dir(absolute)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s: %w\n%s", engine, err, output)
	}
	output := strings.TrimSuffix(absolute, filepath.Ext(absolute)) + ".pdf"
	if _, err := os.Stat(output); err != nil {
		return "", fmt.Errorf("PDF atteso in %s: %w", output, err)
	}
	return output, nil
}

func Diagram(ctx context.Context, root, file string) (string, error) {
	input, err := WorkspaceFile(root, file)
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(input)
	// Write beside the input only after the renderer succeeds.
	out := strings.TrimSuffix(input, ext) + ".svg"
	temp, err := os.CreateTemp(filepath.Dir(input), ".diagram-*.svg")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	var cmd *exec.Cmd
	switch strings.ToLower(ext) {
	case ".mmd", ".mermaid":
		if err := temp.Close(); err != nil {
			return "", err
		}
		cmd = exec.CommandContext(ctx, "mmdc", "-i", input, "-o", temp.Name())
	case ".pikchr", ".pik":
		cmd = exec.CommandContext(ctx, "pikchr", "--svg-only", input)
		cmd.Stdout = temp
	default:
		return "", errors.New("estensioni supportate: .mmd, .mermaid, .pikchr, .pik")
	}
	if cmd.Stdout == nil {
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("mmdc: %w\n%s", err, output)
		}
	} else {
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("pikchr: %w", err)
		}
		if err := temp.Close(); err != nil {
			return "", err
		}
	}
	data, err := os.ReadFile(temp.Name())
	if err != nil || !strings.Contains(string(data), "<svg") {
		return "", errors.New("il renderer non ha generato un SVG")
	}
	return out, os.Rename(temp.Name(), out)
}
