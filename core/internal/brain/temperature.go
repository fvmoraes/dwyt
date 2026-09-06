package brain

import (
	"sort"
	"strings"
)

// HOT / WARM / COLD memory (spec §17).
//
// Temperature is a *retrieval priority*, not a storage tier: every note lives in
// the same vault. It answers "what should be loaded first, and what must never
// be loaded automatically".
//
// The classification is by canonical key and note state, both of which are
// already recorded, so it costs nothing at retrieval time.

// Temperature is the retrieval priority of a memory note.
type Temperature string

const (
	Hot  Temperature = "hot"
	Warm Temperature = "warm"
	Cold Temperature = "cold"
	// All is the sentinel for "no temperature filter".
	All Temperature = ""
)

// ParseTemperature maps a query parameter to a Temperature. An unrecognised
// value means "no filter" rather than an error: a bad filter must not make the
// endpoint fail.
func ParseTemperature(s string) Temperature {
	switch Temperature(strings.ToLower(strings.TrimSpace(s))) {
	case Hot:
		return Hot
	case Warm:
		return Warm
	case Cold:
		return Cold
	}
	return All
}

// hotKeys are loaded first: the current task, what constrains it, and the
// architecture it operates in.
var hotKeys = map[string]bool{
	"project":            true,
	"active-constraints": true,
	"current-task":       true,
	"active-errors":      true,
	"architecture":       true,
}

// warmKeys are loaded when the hot set is not sufficient.
var warmKeys = map[string]bool{
	"conventions":  true,
	"known-issues": true,
	"lessons":      true,
	"error-memory": true,
	"frontend":     true,
	"backend":      true,
	"integrations": true,
}

// TemperatureOf classifies a canonical key together with the note's state.
//
// A resolved or stale note is COLD regardless of its key: it stays searchable but
// must never be loaded automatically, which is the whole point of the
// classification.
func TemperatureOf(key string, state NoteState) Temperature {
	switch state {
	case NoteResolved, NoteStale, NoteDiscardable:
		return Cold
	}
	switch {
	case hotKeys[key]:
		return Hot
	case warmKeys[key]:
		return Warm
	case strings.HasPrefix(key, "module:"):
		return Warm
	case key == "decisions":
		return Hot
	}
	return Cold
}

// temperatureRank orders HOT before WARM before COLD.
func temperatureRank(t Temperature) int {
	switch t {
	case Hot:
		return 0
	case Warm:
		return 1
	default:
		return 2
	}
}

// CanonicalNoteWithTemperature is a canonical note plus its retrieval priority.
type CanonicalNoteWithTemperature struct {
	CanonicalNote
	Temperature Temperature `json:"temperature"`
}

// CanonicalMemory returns the existing canonical notes ordered HOT → WARM →
// COLD. A non-empty filter restricts the result to that temperature.
//
// Notes that do not exist yet are omitted rather than returned empty: a caller
// asking for canonical memory wants what the project actually knows.
func (pb *ProjectObsidian) CanonicalMemory(filter Temperature) []CanonicalNoteWithTemperature {
	keys := CanonicalKeys()
	keys = append(keys, pb.moduleKeys()...)
	keys = append(keys, "decisions")

	var out []CanonicalNoteWithTemperature
	for _, key := range keys {
		note, ok := pb.readCanonicalForMemory(key)
		if !ok {
			continue
		}
		temp := TemperatureOf(key, note.Lifecycle.State)
		if filter != All && temp != filter {
			continue
		}
		out = append(out, CanonicalNoteWithTemperature{CanonicalNote: note, Temperature: temp})
	}

	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := temperatureRank(out[i].Temperature), temperatureRank(out[j].Temperature)
		if ri != rj {
			return ri < rj
		}
		// Within a tier, smaller notes first: they are the cheapest way to add
		// coverage under a budget.
		if out[i].TokensEst != out[j].TokensEst {
			return out[i].TokensEst < out[j].TokensEst
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// readCanonicalForMemory resolves both the layout keys and the append-only
// decision log, which lives at 20-decisions/index.md rather than in
// canonicalLayout.
func (pb *ProjectObsidian) readCanonicalForMemory(key string) (CanonicalNote, bool) {
	if key != "decisions" {
		return pb.ReadCanonical(key)
	}
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	path := joinVault(pb.brainDir, string(AreaDecisions), "index.md")
	content := readFileString(path)
	if strings.TrimSpace(content) == "" {
		return CanonicalNote{Key: key, Path: path}, false
	}
	return CanonicalNote{
		Key:       key,
		Title:     extractTitle(content),
		Body:      bodyOf(content),
		Lifecycle: ParseLifecycle(content),
		Path:      path,
		TokensEst: estimateTokens(content),
	}, true
}

// moduleKeys lists the module summaries present in the vault, as "module:<name>".
func (pb *ProjectObsidian) moduleKeys() []string {
	pb.mu.RLock()
	brainDir := pb.brainDir
	pb.mu.RUnlock()

	entries, err := osReadDir(joinVault(brainDir, string(AreaModules)))
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range entries {
		if !strings.HasSuffix(name, ".md") || name == "index.md" {
			continue
		}
		out = append(out, "module:"+strings.TrimSuffix(name, ".md"))
	}
	sort.Strings(out)
	return out
}
