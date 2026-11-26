package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

func TestTodoFigurePlacementCompletesOnlyTheAssignedComment(t *testing.T) {
	const original = `\documentclass{article}
\begin{document}
\section{Figure}
Prima. % TODO-AI: metti qui la figura
\begin{figure}[tbp]
  \includegraphics{figura.pdf}
  \caption{Figura}
\end{figure}
% TODO-AI: altro lavoro
% TODO VISUAL: bozza
\end{document}
`
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tex"), []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	tasks := parser.Parse("main.tex", []byte(original)).Tasks
	if len(tasks) != 3 || tasks[0].Kind != parser.AITask || tasks[2].Kind != "VISUAL" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	job, err := PrepareTodo(root, tasks[0], "main.tex")
	if err != nil {
		t.Fatal(err)
	}
	job.Result.Text = strings.Replace(job.Original, "[tbp]", "[!htbp]", 1)
	job.Automatic, job.Decision, job.DecisionWhy = true, "Vicino al testo", "Mantiene il contesto"
	next, _, err := Update(job, original)
	if err != nil || !strings.Contains(next, "[!htbp]") || !strings.Contains(next, "Prima.\n") ||
		!strings.Contains(next, "% TODO-AI: altro lavoro") || !strings.Contains(next, "% TODO MANUTENZIONE:") ||
		strings.Contains(next, "% TODO-AI: metti qui la figura") {
		t.Fatalf("unexpected replacement: %v\n%s", err, next)
	}
	job.Result.Text = strings.Replace(job.Result.Text, "{figura.pdf}", "{altro.pdf}", 1)
	if _, _, err := Update(job, original); err == nil {
		t.Fatal("a TODO-AI was allowed to change the figure asset")
	}
}
