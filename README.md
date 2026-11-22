# Notesflow

Notesflow è una CLI/TUI Go per consultare appunti LaTeX, roadmap e TODO. Il watcher aggiorna l’indice locale e il grafo incorporato. Neovim, OpenCode, i compilatori LaTeX e i renderer di diagrammi si collegano tramite i rispettivi comandi.

## Avvio rapido

Richiede Go 1.25 o successivo. La demo funziona con dati locali e senza credenziali.

```sh
go build -o ./notesflow ./cmd/notesflow
./notesflow tui --workspace examples/workspace
```

Nella TUI usa **Tab** o **1–5** per passare tra Dashboard/API, Roadmap, Grafo, TODO e Attività. **↑/↓** o **j/k** scorrono le voci, **c** seleziona il corso, **Invio** apre il file alla riga indicata e **g** apre il grafo. Nella scheda TODO, **f** cambia il tipo, **s** genera il riepilogo e **v** torna alla lista. **q** chiude Notesflow.

Per usare solo il grafo, avvia il server e apri l’URL stampato. Lo stesso processo gestisce il watcher e invia aggiornamenti live al browser tramite SSE.

```sh
./notesflow serve --workspace examples/workspace --open
```

Nel grafo puoi trascinare nodi e sfondo, zoomare, cercare per titolo/corso/file e filtrare gli archi. Il passaggio del puntatore evidenzia i vicini a un salto. Seleziona un nodo per leggere l’anteprima, ispezionare i collegamenti o aprirlo in Neovim. Gli archi LaTeX sono continui, quelli semantici tratteggiati. La demo identifica i collegamenti di tipo semantico come **Affinità locale** nei dettagli.

## Workspace e strumenti

Per impostazione predefinita Notesflow legge `notesflow.json` nella radice del workspace. Copia [notesflow.example.json](notesflow.example.json) come punto di partenza, oppure passalo con `--config`. Le opzioni precedono gli argomenti posizionali.

```sh
./notesflow index --workspace examples/workspace
./notesflow todos --workspace examples/workspace --kind visual
./notesflow todos --workspace examples/workspace --course analisi --section Cauchy --summary
./notesflow compile --workspace examples/workspace --view analisi/main.tex
./notesflow diagram --workspace examples/workspace analisi/workflow.mmd
./notesflow diagram --workspace examples/workspace geometria/metriche.pikchr
./notesflow agent --workspace examples/workspace
```

- **Neovim**. La TUI può avviare direttamente `nvim`. Per il pulsante nel browser, avvia `nvim --listen /tmp/notesflow-nvim.sock` e imposta `tools.nvim_server` allo stesso valore. Il valore di `NVIM` viene usato come default se disponibile.
- **LaTeX e Skim**. `compile` usa `latexmk` per default e supporta anche `pdflatex`, `xelatex`, `lualatex` e `tectonic`. Compila il file principale dalla sua directory e genera informazioni SyncTeX. Con `--view` apre il PDF in Skim su macOS, oppure nel visualizzatore di sistema su altre piattaforme. Su macOS, imposta `pdf_viewer` a una stringa vuota per usare il visualizzatore predefinito.
- **Roadmap**. `roadmap.md` supporta intestazioni, liste e checkbox. L’avanzamento conta le checkbox spuntate. `roadmap.pdf` apre il documento nel visualizzatore e mostra una breve anteprima testuale se `pdftotext` è disponibile.
- **Mermaid e Pikchr**. `diagram` cerca `mmdc` o `pikchr` nel PATH e scrive l’SVG accanto al sorgente. Il grafo incorporato usa Canvas senza Node, CDN o build frontend.
- **OpenCode**. `agent` avvia la sessione interattiva nel workspace. Il driver `opencode` avvia le inferenze in una directory temporanea, con permessi per gli strumenti impostati su deny. Autenticazione e modelli si configurano in OpenCode.

## Provider e delta

In modalità **BYOK** si usa il profilo indicato da `providers.active`. In modalità **pool** si ruota tra i profili configurati, saltando quelli in pausa o a quota esaurita. Rimuovi il profilo `mock` dal pool se vuoi usare solo LLM. Per `openai-compatible`, configura la base API (per esempio con suffisso `/v1`), il modello e il nome della variabile d’ambiente della chiave. `request_limit` e `window` stimano il residuo in base ai tentativi Notesflow. Il sistema rispetta anche `Retry-After` quando il server lo comunica.

Per ogni modifica, Notesflow confronta gli hash delle sezioni del file e costruisce una richiesta per ciascuna sezione cambiata. La richiesta contiene l’estratto della sezione e al massimo `candidate_limit` schede con ID, titolo e sintesi breve di altri nodi. Una modifica soltanto di posizione non richiede una nuova analisi. L’indice di partenza viene popolato alla prima esecuzione. I delta rimasti in coda vengono ripresi al riavvio e gli archi vengono validati rispetto ai nodi correnti. `semantic.enabled: false` sospende l’invio automatico. `todos --summary` è un’azione separata e invia i TODO che corrispondono ai filtri.

Lo stato in `.notesflow/` contiene l’indice dei file, le sezioni testuali, le ultime 500 variazioni degli hash con una sintesi, i risultati semantici, le finestre di quota e gli ultimi cambi di modello. I file JSON sono sostituiti atomicamente. Una sola istanza alla volta aggiorna il workspace. Il server è in ascolto su `127.0.0.1` e l’apertura di file dal browser accetta gli ID del grafo. Il parser indicizza costrutti LaTeX comuni con risoluzione locale dei riferimenti.

## Struttura del codice

| Package | Responsabilità |
| --- | --- |
| `cmd/notesflow` | CLI, configurazione e ciclo di vita |
| `internal/parser` | Sezioni, riferimenti, TODO e roadmap |
| `internal/watcher` | Eventi filesystem, riconciliazione e coda dei delta |
| `internal/graph` | Indice, nodi, archi, snapshot e storico delle revisioni |
| `internal/providers` | Backend, pool, quote e riepiloghi |
| `internal/tui` / `internal/web` | Terminale e grafo Canvas con SSE |
| `internal/workflow` | Neovim, LaTeX, Skim e diagrammi |
| `internal/config` / `internal/storage` | Impostazioni e persistenza JSON |

## Pubblicazione privata su GitHub

Dalla radice della repository locale, autentica `gh`, crea il remoto privato e pubblica il branch corrente. La seconda riga presuppone che esista almeno un commit locale e che il nome sia libero nell’account autenticato.

```sh
gh auth login --hostname github.com
gh repo create notesflow --private --source=. --remote=origin --push
gh repo view --json url,visibility
```

Il risultato atteso per `visibility` è `PRIVATE`.
