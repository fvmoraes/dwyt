package brain

import (
	"testing"
)

func TestTemperatureOfKeys(t *testing.T) {
	cases := map[string]Temperature{
		"project":            Hot,
		"active-constraints": Hot,
		"current-task":       Hot,
		"architecture":       Hot,
		"decisions":          Hot,
		"conventions":        Warm,
		"known-issues":       Warm,
		"lessons":            Warm,
		"module:contextgov":  Warm,
		"unknown-key":        Cold,
	}
	for key, want := range cases {
		if got := TemperatureOf(key, NoteActive); got != want {
			t.Fatalf("TemperatureOf(%q) = %s, want %s", key, got, want)
		}
	}
}

// A resolved or stale note is COLD whatever its key: it stays searchable but
// must never be loaded automatically.
func TestStateForcesCold(t *testing.T) {
	for _, state := range []NoteState{NoteResolved, NoteStale, NoteDiscardable} {
		if got := TemperatureOf("project", state); got != Cold {
			t.Fatalf("state %s should force cold, got %s", state, got)
		}
	}
}

func TestParseTemperature(t *testing.T) {
	if ParseTemperature("HOT") != Hot {
		t.Fatal("case-insensitive parsing failed")
	}
	if ParseTemperature("nonsense") != All {
		t.Fatal("an unrecognised filter must mean 'no filter', not an error")
	}
	if ParseTemperature("") != All {
		t.Fatal("an empty filter must mean 'no filter'")
	}
}

func TestCanonicalMemoryOrdersHotBeforeWarm(t *testing.T) {
	pb := testVault(t)
	if err := pb.EnsureCanonicalLayout(); err != nil {
		t.Fatal(err)
	}
	pb.UpsertCanonical("conventions", "", "table-driven tests\n", SourceRef{})
	pb.UpsertCanonical("architecture", "", "Go plus React\n", SourceRef{})

	notes := pb.CanonicalMemory(All)
	if len(notes) == 0 {
		t.Fatal("seeded canonical notes should be returned")
	}
	seenWarm := false
	for _, n := range notes {
		switch n.Temperature {
		case Warm, Cold:
			seenWarm = true
		case Hot:
			if seenWarm {
				t.Fatalf("a HOT note appeared after a WARM/COLD one: %v", temperatureSequence(notes))
			}
		}
	}
}

func TestCanonicalMemoryFilters(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()

	hot := pb.CanonicalMemory(Hot)
	if len(hot) == 0 {
		t.Fatal("expected at least the seeded project identity in the hot set")
	}
	for _, n := range hot {
		if n.Temperature != Hot {
			t.Fatalf("filter leaked a %s note: %s", n.Temperature, n.Key)
		}
	}
}

func TestCanonicalMemoryOmitsMissingNotes(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()

	for _, n := range pb.CanonicalMemory(All) {
		if n.Body == "" && n.TokensEst == 0 {
			t.Fatalf("an empty placeholder leaked into the result: %s", n.Key)
		}
	}
	// Notes that were never created must not appear at all.
	for _, n := range pb.CanonicalMemory(All) {
		if n.Key == "current-task" {
			t.Fatal("current-task was never written; it must not be reported as canonical memory")
		}
	}
}

func TestCanonicalMemoryIncludesModulesAndDecisions(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()
	pb.UpsertCanonical("module:governor", "", "the context governor\n", SourceRef{})
	if _, err := pb.appendDecisionLog("Adopted the three-MCP architecture"); err != nil {
		t.Fatal(err)
	}

	keys := map[string]bool{}
	for _, n := range pb.CanonicalMemory(All) {
		keys[n.Key] = true
	}
	if !keys["module:governor"] {
		t.Fatalf("module summaries must be part of canonical memory: %v", keys)
	}
	if !keys["decisions"] {
		t.Fatalf("the decision log must be part of canonical memory: %v", keys)
	}
}

func temperatureSequence(notes []CanonicalNoteWithTemperature) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Key+"="+string(n.Temperature))
	}
	return out
}
