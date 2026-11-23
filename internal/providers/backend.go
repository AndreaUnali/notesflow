package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/AndreaUnali/notesflow/internal/config"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

var httpClient = &http.Client{
	Timeout:       45 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

func complete(ctx context.Context, p config.Profile, instructions, data string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if p.Driver == "opencode" {
		return openCode(ctx, p.Model, instructions+"\n\n"+data)
	}
	body, err := json.Marshal(struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}{p.Model, []message{{"system", instructions}, {"user", data}}})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := os.Getenv(p.APIKeyEnv); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return "", errors.New("richiesta HTTP fallita")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		retry := retryAfter(res.Header.Get("Retry-After"))
		if res.StatusCode == http.StatusTooManyRequests && retry.IsZero() {
			retry = time.Now().Add(time.Minute)
		}
		return "", &requestError{message: fmt.Sprintf("HTTP %d", res.StatusCode), retryAt: retry}
	}
	var result struct{ Choices []struct{ Message message } }
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result); err != nil || len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", errors.New("risposta chat/completions non valida o vuota")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func retryAfter(value string) time.Time {
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Now().Add(time.Duration(seconds) * time.Second)
	}
	if timestamp, err := http.ParseTime(value); err == nil {
		return timestamp
	}
	return time.Time{}
}

// Run background analysis in an empty directory with tool permissions denied.
func openCode(ctx context.Context, model, prompt string) (string, error) {
	dir, err := os.MkdirTemp("", "notesflow-opencode-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	args := []string{"run", "--format", "json"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--", prompt)
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "OPENCODE_CONFIG_CONTENT={\"permission\":{\"*\":\"deny\"}}")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = cmd.Start(); err != nil {
		return "", err
	}
	var content strings.Builder
	var eventError bool
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var event struct {
			Type string
			Part struct{ Text string }
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event.Type {
		case "text":
			if content.Len()+len(event.Part.Text) <= 1<<20 {
				content.WriteString(event.Part.Text)
			} else {
				eventError = true
			}
		case "error":
			eventError = true
		}
	}
	if scanner.Err() != nil {
		_ = cmd.Process.Kill()
	}
	err = cmd.Wait()
	if err != nil || eventError || scanner.Err() != nil || strings.TrimSpace(content.String()) == "" {
		return "", errors.New("OpenCode run fallito o senza testo; controllare il profilo e l’autenticazione OpenCode")
	}
	return strings.TrimSpace(content.String()), nil
}
