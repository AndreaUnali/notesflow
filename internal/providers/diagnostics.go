package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type guidedError struct{ cause, help string }

func (e *guidedError) Error() string { return e.cause }

const openCodeCheckModel = "Prova il modello in OpenCode con un messaggio breve. Se usi un router locale, controlla la disponibilità dei suoi modelli."

func statusDiagnosis(status int, openCode bool) (cause, help string) {
	prefix := "Provider"
	login := "Controlla la chiave del profilo in notesflow.json e riapri Notesflow."
	model := "Controlla base_url e model in notesflow.json."
	if openCode {
		prefix = "OpenCode"
		login = "Ricollega il provider con opencode auth login oppure controlla la chiave del router locale."
		model = "Verifica il modello con opencode models e seleziona un modello disponibile."
	}
	switch status {
	case 401, 403:
		return fmt.Sprintf("%s · autenticazione o accesso rifiutati (HTTP %d).", prefix, status), login
	case 402:
		return prefix + " · credito del provider esaurito (HTTP 402).", "Controlla il credito del provider o seleziona un altro profilo."
	case 404:
		return prefix + " · modello o endpoint non trovato (HTTP 404).", model
	case 429:
		return prefix + " · quota o limite di richieste raggiunto (HTTP 429).", "Attendi il ripristino della quota. Se usi un router, verifica quali modelli sono disponibili."
	default:
		if status >= 500 {
			return fmt.Sprintf("%s · servizio del modello indisponibile (HTTP %d).", prefix, status), "Attendi e riprova. Con un router locale, controlla lo stato dei modelli a monte."
		}
		return fmt.Sprintf("%s · richiesta rifiutata (HTTP %d).", prefix, status), model
	}
}

func classifyOpenCode(diagnostic string) (cause, help string) {
	text := strings.ToLower(diagnostic)
	any := func(values ...string) bool {
		for _, value := range values {
			if strings.Contains(text, value) {
				return true
			}
		}
		return false
	}
	switch {
	case any("context window", "context_length", "contextlengthexceeded", "prompt is too long"):
		return "OpenCode · la richiesta supera il contesto del modello.", "Riduci la sezione o seleziona meno pagine dalle fonti."
	case any("no deployments available", "no healthy deployments", "serviceunavailable", "overloaded"):
		return "OpenCode · modelli del provider o del router indisponibili.", "Attendi e riprova. Con un router locale, controlla lo stato dei modelli a monte."
	case any("http 429"):
		return statusDiagnosis(429, true)
	case any("insufficient_quota", "insufficient quota", "rate limit", "ratelimit"):
		return "OpenCode · quota o limite di richieste raggiunto.", "Attendi il ripristino della quota. Se usi un router, verifica quali modelli sono disponibili."
	case any("http 401"):
		return statusDiagnosis(401, true)
	case any("unauthorized", "authenticationerror", "invalid api key", "api key is missing", "providerauth"):
		return "OpenCode · autenticazione del provider rifiutata.", "Ricollega il provider con opencode auth login oppure controlla la chiave del router locale."
	case any("http 404"):
		return statusDiagnosis(404, true)
	case any("model not found", "modelnotfound", "unknown model"):
		return "OpenCode · modello o endpoint non trovato.", "Verifica il modello con opencode models e seleziona un modello disponibile."
	case any("econnrefused", "connection refused", "enotfound", "network error"):
		return "OpenCode non riesce a raggiungere il provider.", "Controlla la connessione e, se usi un router locale, verifica che sia avviato."
	case any("eacces", "eperm", "permission denied", "operation not permitted"):
		return "OpenCode ha segnalato un problema di permessi.", "Esegui opencode --version dallo stesso terminale per verificare l’accesso ai file di OpenCode."
	default:
		return "", ""
	}
}

// Explain gives the activity view a cause and a separate recovery action.
func Explain(err error) (cause, help string) {
	var guided *guidedError
	var request *requestError
	var unavailable *Unavailable
	switch {
	case errors.As(err, &guided):
		return err.Error(), guided.help
	case errors.As(err, &request) && request.status > 0:
		cause, help = statusDiagnosis(request.status, false)
		retry := request.retryAt
		if errors.As(err, &unavailable) && !unavailable.RetryAt.IsZero() {
			retry = unavailable.RetryAt
		}
		if !retry.IsZero() {
			help += " Prossimo tentativo dalle " + retry.Local().Format("15:04:05") + "."
		}
		return cause, help
	case errors.Is(err, context.DeadlineExceeded):
		return "Il provider non ha risposto entro il tempo previsto.", "Verifica che il modello sia disponibile e riprova."
	case errors.As(err, &unavailable):
		if !unavailable.RetryAt.IsZero() {
			return err.Error(), "Riprova dopo le " + unavailable.RetryAt.Local().Format("15:04:05") + " oppure seleziona un altro profilo."
		}
		return err.Error(), "Apri la Dashboard API con Shift+Tab, poi a, e controlla il profilo, la chiave e la quota."
	default:
		return err.Error(), ""
	}
}
