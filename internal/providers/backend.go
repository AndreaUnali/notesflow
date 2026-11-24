package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	if p.Driver == "opencode" {
		return openCode(ctx, p.Model, instructions+"\n\n"+data)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
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
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", context.DeadlineExceeded
		}
		return "", &guidedError{"Il provider non ha risposto alla richiesta HTTP.", "Controlla la connessione e base_url in notesflow.json. Se usi un router locale, verifica che sia avviato."}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		retry := retryAfter(res.Header.Get("Retry-After"))
		if res.StatusCode == http.StatusTooManyRequests && retry.IsZero() {
			retry = time.Now().Add(time.Minute)
		}
		return "", &requestError{message: fmt.Sprintf("HTTP %d", res.StatusCode), retryAt: retry, status: res.StatusCode}
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
