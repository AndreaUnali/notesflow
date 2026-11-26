package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const openCodeTimeout = 90 * time.Second

// Read enough stderr to classify CLI failures without putting subprocess logs in the job.
type diagnosticOutput struct{ strings.Builder }

func (o *diagnosticOutput) Write(data []byte) (int, error) {
	size := min(len(data), max(0, 8192-o.Len()))
	o.Builder.Write(data[:size])
	return len(data), nil
}

type openCodeFault struct {
	Name       string
	Message    string
	StatusCode int
	Data       struct {
		Message    string
		StatusCode int
	}
}

// Run background analysis in an empty directory with tool permissions denied.
func openCode(ctx context.Context, model, prompt string) (string, error) {
	return runOpenCode(ctx, model, prompt, openCodeTimeout)
}

// A recovered turn can have both text and a CLI error. The caller must validate
// the text before using it in that case.
func runOpenCode(ctx context.Context, model, prompt string, timeout time.Duration) (string, error) {
	path, err := exec.LookPath("opencode")
	if err != nil {
		return "", &guidedError{"OpenCode non è disponibile nel PATH.", "Installa OpenCode oppure configura un profilo LLM in notesflow.json."}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "notesflow-opencode-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	args := []string{"--format", "json"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--", prompt)
	cmd := openCodeCommand(ctx, path, dir, []string{"run"}, args...)
	cmd.Env = append(os.Environ(), "OPENCODE_CONFIG_CONTENT={\"permission\":{\"*\":\"deny\"}}")
	var stderr diagnosticOutput
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = cmd.Start(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", &guidedError{"OpenCode non ha completato la richiesta entro il tempo limite.", openCodeCheckModel}
		}
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		return "", fmt.Errorf("avvio di OpenCode: %w", err)
	}
	var content strings.Builder
	var fault openCodeFault
	var faultText string
	var eventError, restarted, oversized bool
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var event struct {
			Type  string
			Part  struct{ Text string }
			Error json.RawMessage
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event.Type {
		case "step_start":
			restarted = eventError
		case "text":
			if content.Len()+len(event.Part.Text) <= 1<<20 {
				content.WriteString(event.Part.Text)
			} else {
				oversized = true
			}
		case "error":
			// A CLI retry can start a fresh step after the failed stream. Drop any
			// partial text from that stream before reading the new response.
			eventError, restarted = true, false
			content.Reset()
			fault, faultText = openCodeFault{}, ""
			var parsed openCodeFault
			if json.Unmarshal(event.Error, &parsed) == nil {
				fault = parsed
			} else {
				_ = json.Unmarshal(event.Error, &faultText)
			}
		}
	}
	if scanner.Err() != nil {
		_ = cmd.Process.Kill()
	}
	err = cmd.Wait()
	answer := strings.TrimSpace(content.String())
	var retriedAnswer string
	if restarted && ctx.Err() == nil && scanner.Err() == nil && !oversized {
		retriedAnswer = answer
	}
	status := max(fault.StatusCode, fault.Data.StatusCode)
	diagnostic := strings.Join([]string{fault.Name, fault.Message, fault.Data.Message, faultText, stderr.String()}, "\n")
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return "", ctx.Err()
	case oversized || scanner.Err() == bufio.ErrTooLong:
		return "", &guidedError{"La risposta di OpenCode supera il limite di lettura.", "Riduci la sezione o l’intervallo di pagine e riprova."}
	case status >= 400:
		cause, help := statusDiagnosis(status, true)
		return retriedAnswer, &guidedError{cause, help}
	}
	if cause, help := classifyOpenCode(diagnostic); cause != "" {
		if eventError || err != nil || answer == "" {
			return retriedAnswer, &guidedError{cause, help}
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", &guidedError{"OpenCode non ha completato la richiesta entro il tempo limite.", openCodeCheckModel}
	}
	if scanner.Err() != nil {
		return "", &guidedError{"Si è interrotta la lettura della risposta di OpenCode.", openCodeCheckModel}
	}
	if eventError || err != nil {
		cause := "OpenCode ha segnalato un errore durante la richiesta."
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() > 0 {
			cause = fmt.Sprintf("OpenCode si è chiuso con codice %d.", exit.ExitCode())
		}
		return retriedAnswer, &guidedError{cause, openCodeCheckModel}
	}
	if answer != "" {
		return answer, nil
	}
	return "", &guidedError{"OpenCode ha terminato senza restituire testo.", openCodeCheckModel}
}
