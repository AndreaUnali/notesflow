package graph

import (
	"math"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/AndreaUnali/notesflow/internal/parser"
)

var (
	texMetadata    = regexp.MustCompile(`\\(?:label|ref|eqref|cite|begin|end|input|include)\*?\{[^}]*\}`)
	texCommandName = regexp.MustCompile(`\\[a-zA-Z]+\*?`)
	topicNumber    = regexp.MustCompile(`^\s*\d+(?:\.\d+)+[.)]?\s+`)
	conceptStop    = topicSet(strings.Fields("a ad al alla alle allo an and as at by che chi cioè col com come con da dal dalla dalle dai de del della delle dello degli dei di does dopo e ed either è era essere fa fra gli has have how i il in is it its l la le lo ma most nei nel nella nelle nello not o of on or per più può qua quali quella questa questo se sede si so sono su sul sulla that the their them these they this those to tra tramite un una under up use via was we were what when where why which who will with you your parte capitolo chapter sezione section subsection paragraph paragrafo appunti descrizione spiegazione summary introduzione introduction esempio example definizioni definitions fondamenti fundamental concetti base basic tecniche techniques tecnica technology tecnologie tecnologia caso case fase overview video slide figure figura table tabella capito significa viene vengono molto meno numero metodo approach approccio note testo linee lavoro appropriate dispose normal typically simply actually available different features feature tools instance many example examples possible often real"))
	topicAside     = regexp.MustCompile(`(?i)\s*:\s*["“«](?:cosa|quanto|come|perché|what|how|why)[^"”»]*["”»]\s*$`)
	controlGroups  = regexp.MustCompile(`(?i)control\s+groups?`)
)

var conceptAliases = map[string]string{
	"applicazione": "application", "applicazioni": "application",
	"anelli": "ring", "anello": "ring", "architettura": "architecture", "architetture": "architecture",
	"architetturale": "architectural", "autorizzazioni": "permission", "avanzato": "advanced",
	"calcolo": "computing", "classificazione": "classification", "comunicazione": "communication",
	"concetto": "concept", "contenitori": "container", "contenitore": "container",
	"controllo": "control", "copia": "copy", "distribuita": "distributed", "distribuito": "distributed",
	"emulazione": "emulation", "fondamentale": "fundamental", "intestazione": "heading",
	"esecuzione": "execution", "estendibilità": "extensibility", "gestione": "management",
	"gerarchia": "hierarchy", "interfacce": "interface", "interfaccia": "interface",
	"isolamento": "isolation", "leggera": "lightweight", "leggero": "lightweight",
	"limiti": "limitation", "limitazioni": "limitation", "logistica": "logistics",
	"macchina": "machine", "macchine": "machine", "memoria": "memory", "migrazione": "migration",
	"modalità": "mode", "modulare": "modular", "modello": "model", "modelli": "model",
	"orchestrazione": "orchestration", "pacchetti": "package", "paginazione": "paging",
	"privilegio": "privilege", "privilegi": "privilege", "processi": "process", "processo": "process",
	"rete": "network", "risorse": "resource", "risorsa": "resource",
	"schema": "diagram", "sicurezza": "security", "sinergica": "synergistic",
	"sistemi": "system", "sistema": "system", "spazio": "space", "spazi": "space",
	"tassonomia": "taxonomy", "tradizionali": "traditional", "tradizionale": "traditional",
	"traduzione": "translation", "utente": "user", "utenti": "user",
	"vms": "vm", "violazione": "violation", "virtuale": "virtual", "virtuali": "virtual",
	"virtualizzazione": "virtualization", "virtualisation": "virtualization",
}

func topicSet(terms []string) map[string]bool {
	out := make(map[string]bool, len(terms))
	for _, term := range terms {
		out[term] = true
	}
	return out
}

// conceptTerms keeps short technical acronyms and normalizes common bilingual terms.
func conceptTerms(value string) map[string]bool {
	value = texCommandName.ReplaceAllString(texMetadata.ReplaceAllString(value, " "), " ")
	value = controlGroups.ReplaceAllString(value, "cgroups")
	out := make(map[string]bool)
	for _, word := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
	}) {
		normalized := strings.ToLower(word)
		if conceptStop[normalized] || !strings.ContainsFunc(word, unicode.IsLetter) {
			continue
		}
		size := len([]rune(word))
		if size < 3 && !(size == 2 && word == strings.ToUpper(word)) {
			continue
		}
		if alias, ok := conceptAliases[normalized]; ok {
			normalized = alias
		} else if size > 3 && strings.HasSuffix(normalized, "s") &&
			!strings.HasSuffix(normalized, "ss") && !strings.HasSuffix(normalized, "us") && !strings.HasSuffix(normalized, "is") {
			normalized = strings.TrimSuffix(normalized, "s")
		}
		if !conceptStop[normalized] {
			out[normalized] = true
		}
	}
	return out
}

type conceptProfile struct {
	title, body map[string]bool
	words       int
	line        int
	pending     bool
}

// Nested material contributes vocabulary and evidence to its containing heading.
func conceptProfiles(docs map[string]parser.Document) map[string]conceptProfile {
	profiles := map[string]conceptProfile{}
	for _, doc := range docs {
		if doc.Roadmap != nil {
			continue
		}
		pending := map[string]bool{}
		for _, task := range doc.Tasks {
			pending[task.NodeID] = true
		}
		for i := len(doc.Sections) - 1; i >= 0; i-- {
			section := doc.Sections[i]
			profile := profiles[section.ID]
			if profile.body == nil {
				profile.body = map[string]bool{}
			}
			profile.title = conceptTerms(section.Title)
			for term := range conceptTerms(contentBeforeComments(section.Text, strings.EqualFold(path.Ext(doc.File), ".tex"))) {
				profile.body[term] = true
			}
			profile.words += section.ContentWords
			if section.ContentLine > 0 && (profile.line == 0 || section.ContentLine < profile.line) {
				profile.line = section.ContentLine
			}
			profile.pending = profile.pending || pending[section.ID] || section.OpenChecklist
			profiles[section.ID] = profile
			if section.Parent == "" {
				continue
			}
			parent := profiles[section.Parent]
			if parent.body == nil {
				parent.body = map[string]bool{}
			}
			for term := range profile.body {
				parent.body[term] = true
			}
			for term := range profile.title {
				parent.body[term] = true
			}
			parent.words += profile.words
			if profile.line > 0 && (parent.line == 0 || profile.line < parent.line) {
				parent.line = profile.line
			}
			parent.pending = parent.pending || profile.pending
			profiles[section.Parent] = parent
		}
	}
	return profiles
}

func contentBeforeComments(text string, tex bool) string {
	if !tex {
		return text
	}
	var content strings.Builder
	for _, line := range strings.Split(text, "\n") {
		code, _, _ := strings.Cut(line, "%")
		content.WriteString(code)
		content.WriteByte('\n')
	}
	return content.String()
}

type termFrequency struct {
	total  int
	counts map[string]int
}

func profileFrequency(profiles []conceptProfile) termFrequency {
	counts := map[string]int{}
	for _, p := range profiles {
		for term := range p.body {
			counts[term]++
		}
	}
	return termFrequency{total: len(profiles), counts: counts}
}

func (f termFrequency) weight(term string) float64 {
	if f.total < 3 {
		return 1
	}
	return 1 + math.Log(float64(f.total+1)/float64(f.counts[term]+1))
}

func sameTerms(a, b map[string]bool) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for term := range a {
		if !b[term] {
			return false
		}
	}
	return true
}
