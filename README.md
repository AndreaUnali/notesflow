# Notesflow

Scrivi appunti LaTeX, controlla il PDF e segui i collegamenti tra i concetti dalla stessa sessione nel terminale. La home riunisce azioni, TODO, anteprima e roadmap. Il grafo e l’editor si aprono nella stessa interfaccia.

## Installa e prova

Su macOS con Homebrew:

```sh
brew install go neovim poppler gh
gh repo clone AndreaUnali/notesflow
cd notesflow
go build -o notesflow ./cmd/notesflow
./notesflow --workspace examples/workspace/analisi
```

Accedi con `gh auth login` se GitHub CLI lo richiede. Se ti manca `latexmk`, puoi installare MacTeX con `brew install --cask mactex-no-gui` e riaprire il terminale. Premi **b** per compilare l’esempio e popolare l’anteprima. Ghostty e Kitty mostrano il grafo e le pagine PDF a risoluzione grafica. Negli altri terminali puoi leggere il testo del PDF e navigare i collegamenti dalla lista del grafo.

## Il primo giro

1. Avvia `./notesflow` e scrivi il nome di una **Note Network**. Premi **Ctrl+O** per aggiungere una cartella esistente, oppure incolla il percorso nella ricerca. Se hai già un progetto in mente, usa `./notesflow --workspace /percorso/del/corso`.
2. In Home, **Riprendi** riapre file e cursore dell’ultima sessione. **Nuovo capitolo** propone il file e il punto in cui includerlo nel documento principale. TODO, PDF e roadmap sono visibili dalla Home.
3. Premi **2** per scrivere. Il PDF occupa la colonna destra. Salva con **:w** e compila con **Shift+Tab**, poi **b**.
4. Premi **Shift+Tab**, poi **3**, per esplorare il grafo con il buffer aperto. Usa **↑/↓** e **Invio** per aprire un nodo, **Esc** per tornare alla panoramica e **/** per cercare una sezione.

![Scrittura nella TUI con la colonna PDF estesa](docs/screenshots/editor.png)

![Grafo neon con nodi etichettati, gerarchia e connessioni semantiche](docs/screenshots/graph.png)

### Comandi

| Tasto | Azione |
| --- | --- |
| **1–5** | Home, scrittura, grafo, TODO, attività. Le schede si possono anche cliccare. |
| **Shift+Tab** | Porta il focus alle schede dall’editor o da un pannello. Usa **1–5** o **←/→** e **Invio**. **Tab** torna al pannello. |
| **r** / **R** | Seleziona le tappe / mostra o comprimi la roadmap. |
| **t** | Seleziona il riquadro TODO. **←/→** passa fra Tutti, Classici, Visual e Manutenzione. |
| **p** | Seleziona il PDF. **←/→** cambia pagina e **t** alterna pagina e testo. |
| **b** / **c** / **q** | Compila / cambia corso / esci. |

## Roadmap e verifiche

Crea `roadmap.md` nella cartella del corso. Notesflow usa le checkbox oppure, in una roadmap senza checkbox, i titoli numerati come `### 2.1 Virtualizzazione`. La barra si aggiorna quando salvi gli appunti. I titoli e il testo delle sezioni forniscono l’evidenza, anche quando roadmap e appunti usano terminologia tecnica in italiano e in inglese. Per indicare esattamente quale sezione verificare, aggiungi un link `[verifica]` al relativo file e alla sua etichetta LaTeX o intestazione Markdown.

```markdown
- [ ] Definire la convergenza [verifica](successioni.tex#sec:successioni)
- [ ] Formalizzare il criterio di Cauchy
```

Una casella `[x]` negli appunti Markdown o un commento LaTeX `% DONE:` con la stessa descrizione conferma la tappa. Le corrispondenze dubbie vengono completate nella barra con un **!** e aggiungono un **TODO Manutenzione** con il motivo, il file e la riga degli appunti.

Premi **r** per selezionare la tappa, **e** per aprire l’evidenza negli appunti o **Invio** per aprire la roadmap. Conferma con `[x]` oppure aggiungi `<!-- notesflow: pending -->` in coda alla tappa per tenerla aperta. Notesflow aggiorna lo stato nella TUI al salvataggio.

## Esplora il grafo

La panoramica mostra i concetti principali di ogni capitolo, i riferimenti LaTeX e una selezione dei collegamenti più forti. Apri un capitolo per vedere le altre sezioni. Usa **Pg↑/Pg↓** se un capitolo occupa più pagine e **Invio** per aprire la riga sorgente. La ricerca trova anche le intestazioni minute.

Ogni concetto ha fino a tre relazioni semantiche. La panoramica ne mostra una selezione, e nel dettaglio puoi vedere quelle del nodo selezionato o sotto il puntatore. I legami locali si basano sul lessico distintivo dei titoli e degli appunti. La gerarchia e i riferimenti LaTeX restano consultabili anche senza un provider AI.

## Lavora con l’agente

Configura un profilo LLM in [notesflow.example.json](notesflow.example.json), oppure installa e configura OpenCode nel tuo terminale. Quando il profilo è quello locale, Notesflow usa l’OpenCode già configurato per **Chiedi** e **Delega**. Grafo, TODO e compilazione funzionano anche senza un modello remoto.

- Da **Scrittura**, premi **Shift+Tab**, poi **?**. Chiedi un aiuto sulla sezione corrente, su una formula o sull’ultima compilazione. La risposta è in **Attività**. Usa **f** per continuare o **w** per chiedere di applicare la proposta.
- Da **Home**, seleziona **Delega**. Scegli un file o una sezione con **←/→**, inserisci la richiesta e premi **Tab** per scegliere le fonti. Usa **Invio** per aggiungere il risultato evidenziato o digita un intervallo come `slide.pdf:12-14`. **Ctrl+S** invia.
- L’agente riceve il testo della destinazione e le fonti selezionate. Il risultato si applica quando il blocco assegnato è ancora quello di partenza. Se stai scrivendo altrove nello stesso buffer, salvi tutto insieme con **:w**. In caso di conflitto apri **Attività** per confrontare proposta e appunti.
- Quando l’agente ha una domanda appare una notifica in alto a destra. Cliccala o usa **5 Attività** per scegliere. Dopo 90 secondi si applica la scelta consigliata e compare un **TODO Manutenzione**. Puoi cambiare il tempo con `agent.question_seconds`. **Spazio** mette in pausa o riprende una delega, **s** aggiunge una fonte e **f** invia un messaggio.
- Le figure suggerite compaiono come **TODO Visual** con il riferimento alla slide o la descrizione del diagramma. A delega completata, la notifica apre il riepilogo con fonti e modifiche.

Per aprire direttamente OpenCode, usa `notesflow agent --workspace /percorso/del/corso`.

## Ghostty a schermo intero

Aggiungi queste righe al file di configurazione di Ghostty.

```ini
macos-non-native-fullscreen = true
window-padding-x = 0
window-padding-y = 0
window-padding-color = extend-always
```

Scegli **Ghostty → Reload Configuration**, apri una nuova finestra e premi **⌘ + Invio**. In finestra, Notesflow imposta il titolo con il nome del corso.

## Usa i tuoi appunti

All’apertura di un progetto esistente, Notesflow cerca il documento principale e il PDF e ricorda le scelte. Puoi indicarli anche in `notesflow.json` con `tools.main_tex` e `tools.pdf`.

Per la vista Canvas nel browser usa `notesflow serve --workspace /percorso/del/corso --open`. Per compilazione, export di diagrammi Mermaid/Pikchr e altre operazioni da shell usa `notesflow --help`.
