package agent

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/AndreaUnali/notesflow/internal/parser"
	"github.com/AndreaUnali/notesflow/internal/workflow"
)

type TodoWork struct {
	Line, Occurrence int
	MainTeX          string
}

var ErrTodoMoved = errors.New("il TODO-AI è stato spostato o modificato")
var figurePlacement = regexp.MustCompile(`(\\begin\s*\{\s*figure\*?\s*\})\s*\[[!htbpH \t]+\]`)

func PrepareTodo(root string, task parser.Task, main string) (Job, error) {
	target := Target{File: task.File, Section: task.NodeID, Title: task.Section, Line: task.Line}
	if target.Section == task.File {
		target.Section = ""
	}
	job := Job{Goal: task.Text, Target: target, Write: true, Todo: &TodoWork{Line: task.Line, MainTeX: main}}
	file, err := workflow.WorkspaceFile(root, task.File)
	if err != nil {
		return job, err
	}
	info, err := os.Stat(file)
	if err != nil {
		return job, err
	}
	if info.Size() > 2<<20 {
		return job, errors.New("il sorgente del TODO-AI supera 2 MiB")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return job, err
	}
	if !slices.ContainsFunc(todoMarkers(task.File, string(data)), func(current parser.Task) bool {
		return current.Line == task.Line && current.Text == task.Text && current.NodeID == task.NodeID
	}) {
		return job, ErrTodoMoved
	}
	span, err := Locate(target, string(data))
	if err != nil {
		return job, err
	}
	job.Original = strings.Join(span.Lines[span.Start:span.End], "\n")
	if len(job.Original) > 24000 {
		return job, errors.New("il blocco del TODO-AI è troppo lungo. Inserisci il TODO in una sezione più breve")
	}
	for _, marker := range todoMarkers(task.File, job.Original) {
		if marker.Text == task.Text {
			job.Todo.Occurrence++
		}
		if marker.Line == task.Line-span.Start {
			return job, nil
		}
	}
	return job, ErrTodoMoved
}

func todoMarkers(file, text string) []parser.Task {
	tasks := parser.Parse(file, []byte(text)).Tasks
	return slices.DeleteFunc(tasks, func(task parser.Task) bool { return task.Kind != parser.AITask })
}

func completeTodo(job Job, text string) (string, error) {
	if text == strings.TrimRight(job.Original, "\n") {
		return "", errors.New("l’agente non ha modificato il sorgente del TODO-AI")
	}
	before, after := todoMarkers(job.Target.File, job.Original), todoMarkers(job.Target.File, text)
	if !slices.EqualFunc(before, after, func(a, b parser.Task) bool { return a.Text == b.Text }) {
		return "", errors.New("la proposta cambia i commenti TODO-AI")
	}
	occurrence := 0
	for _, marker := range after {
		if marker.Text != job.Goal {
			continue
		}
		occurrence++
		if occurrence == job.Todo.Occurrence {
			lines := strings.Split(text, "\n")
			index := marker.Line - 1
			lines[index] = parser.ClearAITask(lines[index])
			return strings.Join(lines, "\n"), nil
		}
	}
	return "", ErrTodoMoved
}
