package network

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

func MainFiles(nodes []parser.Node) []string {
	var files []string
	for _, node := range nodes {
		if !strings.EqualFold(filepath.Ext(node.File), ".tex") {
			continue
		}
		switch strings.ToLower(filepath.Base(node.File)) {
		case "main.tex", "master.tex", "root.tex":
			files = append(files, node.File)
		default:
			if strings.Contains(node.Summary, `\documentclass`) {
				files = append(files, node.File)
			}
		}
	}
	slices.Sort(files)
	return slices.Compact(files)
}

func PDFs(root string, mainFiles []string) []string {
	directories := map[string]bool{".": true}
	for _, file := range mainFiles {
		directories[filepath.Dir(file)] = true
	}
	var files []string
	for dir := range directories {
		for _, output := range []string{"", "build", "out"} {
			relative := filepath.Join(dir, output)
			entries, _ := os.ReadDir(filepath.Join(root, relative))
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".pdf") || parser.IsRoadmap(name) {
					continue
				}
				file := filepath.ToSlash(filepath.Join(relative, name))
				if _, err := workflow.WorkspaceFile(root, file); err == nil {
					files = append(files, file)
				}
			}
		}
	}
	slices.Sort(files)
	return slices.Compact(files)
}
