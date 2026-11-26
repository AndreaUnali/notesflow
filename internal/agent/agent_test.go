package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

type guideFunc func(context.Context, Request) (Draft, error)

func (fn guideFunc) Guide(ctx context.Context, req Request) (Draft, error) {
	return fn(ctx, req)
}

func TestRunRecoversFromIncompleteQuestion(t *testing.T) {
	for _, withDraft := range []bool{true, false} {
		job := Job{Write: true}
		calls := 0
		backend := guideFunc(func(_ context.Context, req Request) (Draft, error) {
			calls++
			if req.CanAsk != (calls == 1) {
				t.Fatalf("call %d: CanAsk = %t", calls, req.CanAsk)
			}
			if calls == 1 && !withDraft {
				return Draft{Summary: "Ho una domanda", Question: &Question{}}, nil
			}
			return Draft{Summary: "Bozza pronta", Text: "Contenuto", Question: &Question{}}, nil
		})
		got, err := Run(t.Context(), t.TempDir(), backend, job, nil)
		if err != nil || got.Text != "Contenuto" || got.Question != nil || !got.QuestionFallback {
			t.Fatalf("draft %+v, error %v", got, err)
		}
		if (calls == 1) != withDraft {
			t.Fatalf("withDraft=%t, %d calls", withDraft, calls)
		}
		job.Result = got
		if tasks := Tasks(job); len(tasks) != 1 || tasks[0].Kind != parser.MaintenanceTask {
			t.Fatalf("expected one maintenance TODO, got %+v", tasks)
		}
	}
}

func TestSourcesForAddsMentionedRoadmap(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const roadmap = "Capitolo 1: Altro\nContenuto estraneo\nCapitolo 2: Il capitolo\n## Obiettivi\nCose da fare\nCapitolo 3: Poi"
	if err := os.WriteFile(filepath.Join(root, "roadmap.md"), []byte(roadmap), 0600); err != nil {
		t.Fatal(err)
	}
	job := Job{Goal: "Segui la sezione omonima nella roadmap.", Target: Target{Title: "Il Capitolo"}}
	got := SourcesFor(root, job)
	if len(got) != 1 || got[0].File != "roadmap.md" {
		t.Fatalf("expected roadmap source, got %+v", got)
	}
	materials, err := readMaterials(t.Context(), root, got)
	if err != nil || len(materials) != 1 || materials[0].Text != "Capitolo 2: Il capitolo\n## Obiettivi\nCose da fare" {
		t.Fatalf("expected matching chapter only, got %+v, %v", materials, err)
	}
	job.Goal = "Scrivi un esempio."
	if got := SourcesFor(root, job); len(got) != 0 {
		t.Fatalf("unexpected sources %+v", got)
	}
}

func TestOpenExplainsAnInterruptedRequest(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".notesflow"), 0700); err != nil {
		t.Fatal(err)
	}
	book, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range []Job{
		{State: Working, Result: Draft{Question: &Question{Text: "A stale question"}}},
		{State: Paused},
		{State: Paused, Error: "Riprendi la richiesta da Attività."},
	} {
		if err := book.Store(job); err != nil {
			t.Fatal(err)
		}
	}
	book, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, reason := range []string{sessionEnded, ManualPause, sessionEnded} {
		job := book.Jobs[i]
		if job.State != Paused || job.PauseReason != reason || job.Error != "" || job.Result.Question != nil {
			t.Errorf("job %d: %+v", job.ID, job)
		}
	}
}

func TestRunReadsThreeWholePDFDecks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for name, script := range map[string]string{
		"pdfinfo": "#!/bin/sh\nprintf 'Pages: 64\\n'\n",
		"pdftotext": `#!/bin/sh
page="$2"
while [ "$page" -le "$4" ]; do
  printf 'Deck page %s\n' "$page"
  if [ "$page" -ne 12 ]; then
    i=0
    while [ "$i" -lt 140 ]; do
      printf 'Protocollo e virtualizzazióne. '
      i=$((i + 1))
    done
  fi
  printf '\f'
  page=$((page + 1))
done
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	job := Job{Goal: "Spiega il protocollo."}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("slide %d.pdf", i)
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
		job.Sources = append(job.Sources, Ref{File: name})
	}
	pages := make(map[string]string)
	requests := 0
	backend := guideFunc(func(_ context.Context, request Request) (Draft, error) {
		requests++
		if request.ReadingBytes > 0 {
			source := request.Sources[0]
			if len(request.Sources) != 1 || request.Goal != job.Goal || request.CanAsk || !utf8.ValidString(source.Text) {
				t.Fatalf("unexpected request %+v", request)
			}
			pages[source.File] += source.Text
			return Draft{Summary: "Appunti con riferimento alle pagine. " + strings.Repeat("è", request.ReadingBytes)}, nil
		}
		if len(request.Sources) != 3 {
			t.Fatalf("expected 3 decks, got %d", len(request.Sources))
		}
		for _, source := range request.Sources {
			if len(source.Text) > maxSourceBytes/3 || !utf8.ValidString(source.Text) {
				t.Fatalf("invalid consolidated source %s", source.File)
			}
			for page := 1; page <= 64; page++ {
				if !strings.Contains(pages[source.File], fmt.Sprintf("[Pagina %d]\n", page)) {
					t.Errorf("%s missing page %d", source.File, page)
				}
			}
		}
		return Draft{Summary: "Risposta"}, nil
	})
	if _, err := Run(t.Context(), root, backend, job, nil); err != nil || requests <= 3 {
		t.Fatalf("requests %d, error %v", requests, err)
	}
	rangeRef := Ref{File: job.Sources[0].File, First: 12, Last: 40}
	materials, err := readMaterials(t.Context(), root, []Ref{rangeRef})
	if err != nil || !strings.HasPrefix(materials[0].Text, "[Pagina 12]\n") ||
		!strings.Contains(materials[0].Text, "[Pagina 40]\n") || strings.Contains(materials[0].Text, "[Pagina 41]\n") {
		t.Fatalf("unexpected range: %v", err)
	}
	rangeRef.Last = 65
	if _, err := readMaterials(t.Context(), root, []Ref{rangeRef}); err == nil || !strings.Contains(err.Error(), "64 pagine") {
		t.Fatalf("unexpected out-of-range result: %v", err)
	}
}
