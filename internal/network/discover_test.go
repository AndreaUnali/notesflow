package network

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverAndSearchFindUnopenedLaTeXNetworks(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(base, "Cloud_Tech", "latex")
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}
	main := `\documentclass{article}
\begin{document}
\begin{titlepage}
  {\Huge\bfseries Cloud Computing\par}
\end{titlepage}
\end{document}
`
	if err := os.WriteFile(filepath.Join(folder, "main.tex"), []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(base, "frammenti")
	if err := os.Mkdir(other, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "sezione.tex"), []byte(`\section{Only a fragment}`), 0644); err != nil {
		t.Fatal(err)
	}
	var entries []Recent
	for entry := range Discover(t.Context(), []SearchRoot{{base, 4}}) {
		entries = append(entries, entry)
	}
	if len(entries) != 1 {
		t.Fatalf("expected the LaTeX project only, got %v", entries)
	}
	for _, query := range []string{"cloud computing", "cloud tech", "computing cloud"} {
		found := Search(entries, query)
		if len(found) != 1 || found[0].Root != folder || found[0].Name != "Cloud Computing" {
			t.Fatalf("query %q found %v", query, found)
		}
	}
}
