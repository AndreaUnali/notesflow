package agent

import (
	"os"
	"path/filepath"
	"testing"
)

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
