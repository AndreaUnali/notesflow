<h1 align="center">
  <img src="docs/notesflow-wordmark.png" width="392" height="55" alt="Notesflow">
</h1>

Scrivi appunti LaTeX, controlla il PDF e segui i collegamenti tra i concetti dalla stessa sessione nel terminale. La home riunisce azioni, TODO, anteprima e roadmap. Il grafo e l’editor si aprono nella stessa interfaccia.

![Home del corso con azioni, TODO, anteprima PDF e roadmap](docs/screenshots/home.png)

## Installa e prova

### Homebrew

```sh
brew install AndreaUnali/notesflow/notesflow
```

### Debian e Ubuntu

Scarica e installa il [pacchetto della release](https://github.com/AndreaUnali/notesflow/releases/latest) con APT. Sono disponibili build per amd64 e arm64.

```sh
curl -fL -o notesflow.deb "https://github.com/AndreaUnali/notesflow/releases/latest/download/notesflow-linux-$(dpkg --print-architecture).deb" && sudo apt install ./notesflow.deb
```

### Apri Notesflow

```sh
notesflow
notesflow --workspace /percorso/del/corso
```

Il primo comando apre il selettore dei progetti da qualsiasi cartella. Il secondo apre direttamente il corso. Il pacchetto Debian include Neovim e Poppler. Se usi Homebrew e ti mancano questi strumenti, installali con `brew install neovim poppler` per scrivere e vedere i PDF.

Per compilare LaTeX serve anche `latexmk`. Su macOS puoi installarlo con `brew install --cask mactex-no-gui` e riaprire il terminale. Su Debian e Ubuntu puoi usare `sudo apt install latexmk texlive-latex-extra`. In Notesflow premi **Shift+Tab**, poi **b**, per compilare e aggiornare l’anteprima. Ghostty e Kitty mostrano grafo e PDF a risoluzione grafica.

Per aggiornare con Homebrew usa `brew upgrade AndreaUnali/notesflow/notesflow`. Su Debian e Ubuntu ripeti il comando di installazione per scaricare il pacchetto più recente.

<details>
<summary>Compila dal sorgente e prova il corso di esempio</summary>

```sh
git clone https://github.com/AndreaUnali/notesflow.git
cd notesflow
go run ./cmd/notesflow --workspace examples/workspace/analisi
```

</details>

## Il primo giro

![Schermata di avvio con ricerca, progetti recenti e grafi degli appunti](docs/screenshots/launcher.png)

Al primo avvio puoi scegliere il tema e collegare un provider compatibile con OpenAI tramite **API diretta**, oppure usare OpenCode. Il catalogo ti mostra i modelli dichiarati dal collegamento. Scegline uno e invia un breve messaggio di prova per verificare che risponda. La chiave del provider diretto viene salvata nel file privato delle impostazioni utente. Puoi riaprire la configurazione dal selettore con **F2** o con `notesflow setup`.

In **Aspetto**, usa **←/→** oppure **Invio** sulla palette per vederne subito il nome e i campioni. Seleziona **Applica palette e torna** per usare i nuovi colori nel selettore, nei progetti, in Neovim e nel grafo del browser. Le dieci palette di Notesflow sono adattate da [Catppuccin Mocha](https://catppuccin.com/palette/), [Dracula](https://draculatheme.com/contribute), [Nord](https://www.nordtheme.com/docs/colors-and-palettes/), [Gruvbox Dark](https://github.com/morhetz/gruvbox), [Tokyo Night](https://github.com/folke/tokyonight.nvim), [Solarized Dark](https://ethanschoonover.com/solarized/), [One Dark](https://github.com/atom/one-dark-syntax), [Rosé Pine](https://rosepinetheme.com/palette/), [Kanagawa Wave](https://github.com/rebelot/kanagawa.nvim) e [GitHub Dark](https://github.com/primer/github-vscode-theme).

Dal selettore premi **Ctrl+N**, inserisci il titolo e scegli la struttura. Notesflow crea una cartella con quel titolo in `~/Documents/Notesflow`. **File unico** tiene sorgente, immagini e PDF nella cartella principale. **A capitoli** crea anche `config.tex`, `capitoli/`, `ausiliari/` e `immagini/`. Notesflow prova a compilare subito e aggiorna il PDF del progetto solo dopo una compilazione riuscita.

Quando apri per la prima volta una cartella LaTeX esterna, Notesflow crea una copia in `~/Documents/Notesflow` e prova a compilarla. Un solo sorgente produce un progetto a file unico. Con più sorgenti, Notesflow organizza la copia a capitoli, aggiorna i riferimenti ai file che sposta e conserva le fonti e gli altri allegati. Le aperture successive della cartella originale riprendono la copia di lavoro.

1. Avvia `notesflow` e scrivi il nome di una **Note Network**. La ricerca include i recenti e i progetti LaTeX in Scrivania, Documenti e nella cartella corrente. Premi **Ctrl+O** per aggiungere una cartella esistente, oppure incolla il percorso nella ricerca. Se hai già un progetto in mente, usa `notesflow --workspace /percorso/del/corso`.
2. In Home, **Riprendi** riapre file e cursore dell’ultima sessione. **Nuovo capitolo** propone il file e il punto in cui includerlo nel documento principale. TODO, PDF e roadmap sono visibili dalla Home.
3. Premi **Shift+Tab**, poi **2**, per scrivere. Il PDF occupa la colonna destra. Salva con **:w** e compila con **Shift+Tab**, poi **b**.
4. Premi **Shift+Tab**, poi **3**, per esplorare il grafo con il buffer aperto. Usa **↑/↓** e **Invio** per aprire un nodo, **Esc** per tornare alla panoramica e **/** per cercare una sezione.

![Scrittura LaTeX in Neovim con anteprima PDF estesa e roadmap compressa](docs/screenshots/editor.png)

## Navigare e uscire

Premi **Shift+Tab** per raggiungere la barra di navigazione da qualunque vista, anche mentre scrivi in Neovim, cerchi nel grafo o compili un modulo. I tasti qui sotto si premono dopo aver attivato la barra.

| Tasto | Azione |
| --- | --- |
| **1–5** | Apri Home, Scrittura, Grafo, TODO o Attività. Puoi anche scegliere con **←/→** e confermare con **Invio**. |
| **Tab** o **Esc** | Torna al pannello o al campo da cui sei partito. |
| **q** | Torna al selettore dei progetti. |
| **s** | Salva tutti i file modificati nell’editor aperto. |
| **r** / **R** | Seleziona una tappa o espandi e comprimi la roadmap. Quando è compressa, condivide la barra in basso con scorciatoie, messaggi e stato. |
| **t** | Seleziona i TODO. **←/→** passa fra Tutti, Classici, Visual e Manutenzione. |
| **p** | Seleziona il PDF. **←/→** cambia pagina e **t** alterna pagina e testo. |
| **b** / **c** / **?** | Compila il PDF, cambia corso o chiedi all’agente. |
| **g** | Apri il dettaglio dell’agente da qualsiasi vista. |

Per tornare ai progetti, usa **Shift+Tab**, poi **q**, oppure clicca **q Progetti**. Dal selettore puoi aprire le impostazioni o premere **q** con la ricerca vuota per chiudere Notesflow. Se ci sono modifiche non salvate, il progetto rimane aperto. Usa **Salva** in alto anche dalla Home, poi torna al selettore. In Scrittura puoi continuare a usare **:w** o **:wall**.

## Roadmap e verifiche

Crea `roadmap.md` nella cartella del corso. Notesflow usa le checkbox oppure, in una roadmap senza checkbox, i titoli numerati come `### 2.1 Virtualizzazione`. La barra si aggiorna quando salvi gli appunti. I titoli e il testo delle sezioni forniscono l’evidenza, anche quando roadmap e appunti usano terminologia tecnica in italiano e in inglese. Per indicare esattamente quale sezione verificare, aggiungi un link `[verifica]` al relativo file e alla sua etichetta LaTeX o intestazione Markdown.

```markdown
- [ ] Definire la convergenza [verifica](successioni.tex#sec:successioni)
- [ ] Formalizzare il criterio di Cauchy
```

Una casella `[x]` negli appunti Markdown o un commento LaTeX `% DONE:` con la stessa descrizione conferma la tappa. Le corrispondenze dubbie vengono completate nella barra con un **!** e aggiungono un **TODO Manutenzione** con il motivo, il file e la riga degli appunti.

Premi **Shift+Tab**, poi **r**, per selezionare la tappa, **e** per aprire l’evidenza negli appunti o **Invio** per aprire la roadmap. Conferma con `[x]` oppure aggiungi `<!-- notesflow: pending -->` in coda alla tappa per tenerla aperta. Notesflow aggiorna lo stato nella TUI al salvataggio.

## Esplora il grafo

![Grafo del corso con capitoli, concetti collegati e anteprima PDF](docs/screenshots/graph.png)

La panoramica mostra i concetti principali di ogni capitolo, i riferimenti LaTeX e una selezione dei collegamenti più forti. Apri un capitolo per vedere le altre sezioni. Usa **Pg↑/Pg↓** se un capitolo occupa più pagine e **Invio** per aprire la riga sorgente. La ricerca trova anche le intestazioni minute.

Ogni concetto ha fino a tre relazioni semantiche. La panoramica ne mostra una selezione, e nel dettaglio puoi vedere quelle del nodo selezionato o sotto il puntatore. I legami locali si basano sul lessico distintivo dei titoli e degli appunti. La gerarchia e i riferimenti LaTeX restano consultabili anche senza un provider AI.

## Lavora con l’agente

Configura un profilo LLM in [notesflow.example.json](notesflow.example.json), oppure installa e configura OpenCode nel tuo terminale. Quando il profilo è quello locale, Notesflow usa l’OpenCode già configurato per **Chiedi** e **Delega**. Grafo, TODO e compilazione funzionano anche senza un modello remoto.

- **5 Attività** mostra le deleghe, le domande, i TODO-AI, i riepiloghi e l’analisi del grafo. Seleziona un’attività con **Invio** per aprire la richiesta, la destinazione, le fonti e la cronologia, dal passaggio più recente. Le fasi riportano esito, durata e, quando disponibili, token. Il tasto **l** dalla lista apre il registro tecnico.
- Lo stato dell’agente è sempre in alto a destra. Cliccalo oppure premi **Shift+Tab**, poi **g**, per aprire il dettaglio con la fase, le fonti e l’esito. Se compare **Errore**, trovi in cima la causa e cosa controllare. **Invio** riprova la stessa delega.
- Da **Scrittura**, premi **Shift+Tab**, poi **?**. Chiedi un aiuto sulla sezione corrente, su una formula o sull’ultima compilazione. La risposta è in **Attività**. Usa **f** per continuare o **w** per chiedere di applicare la proposta.
- Da **Home**, seleziona **Delega**. Scegli un file o una sezione con **←/→**, inserisci la richiesta e premi **Tab** per scegliere le fonti. Premi **Invio** sul PDF evidenziato per aggiungerlo intero. Per scegliere alcune slide premi **→**, scrivi una pagina come `12` o un intervallo come `12-28` e premi **Invio**. Puoi aggiungere più PDF prima di inviare con **Ctrl+S**.
- L’agente riceve il testo della destinazione e le fonti selezionate. Il risultato si applica quando il blocco assegnato è ancora quello di partenza. Se stai scrivendo altrove nello stesso buffer, salvi tutto insieme con **:w**. In caso di conflitto apri **Attività** per confrontare proposta e appunti.
- Quando l’agente ha una domanda appare una notifica in alto a destra. Cliccala o premi **Shift+Tab**, poi **5**, per scegliere. Dopo 90 secondi si applica la scelta consigliata e compare un **TODO Manutenzione**. Puoi cambiare il tempo con `agent.question_seconds`. Nel dettaglio, **P** mette in pausa, **Invio** riprende o riprova, **s** aggiunge una fonte e **f** invia un messaggio. Se chiudi Notesflow durante la richiesta, la delega resta in pausa al prossimo avvio.
- Le figure suggerite compaiono come **TODO Visual** con il riferimento alla slide o la descrizione del diagramma. A delega completata, la notifica apre il riepilogo con fonti e modifiche.

Per aprire direttamente OpenCode, usa `notesflow agent --workspace /percorso/del/corso`.

Per lasciare un compito all’agente, scrivi e salva un commento LaTeX nella sezione interessata. Per esempio:

```latex
% TODO-AI: sposta la figura subito dopo questo paragrafo
```

I **TODO-AI** appaiono nel filtro **AI**. L’agente li prende uno alla volta quando è libero e può chiederti una scelta in **Attività**. Scaduto il tempo di risposta, usa la scelta consigliata e lascia un **TODO Manutenzione**. Notesflow prova a compilare la correzione su una copia prima di applicarla e chiudere il TODO. Se la compilazione fallisce o il file è cambiato, la richiesta resta aperta e il motivo compare in **Attività**.

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
