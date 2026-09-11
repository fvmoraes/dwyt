// Package toolopt implements the Tool Output Optimizer (spec §30–§31).
//
// The rule is "summary first, raw on demand". Large tool output is reduced
// deterministically — no LLM involved — to status, errors, counts and the
// evidence needed to act, while the full bytes stay retrievable through the
// Raw Object Store.
//
// The compressor is deliberately conservative. It never removes a line it
// cannot classify, and every compaction carries a raw reference, because
// dropping evidence to save tokens is the one failure mode the spec forbids
// outright (§65: "compression never removes critical evidence without a
// raw_ref").
package toolopt

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Severity classifies a captured diagnostic line.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Diagnostic is one structured finding extracted from tool output.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Column   int      `json:"column,omitempty"`
	Code     string   `json:"code,omitempty"`
	Message  string   `json:"message"`
	// Count is how many times an equivalent diagnostic appeared. Repeated
	// identical errors collapse into one entry with a count (spec §30).
	Count int `json:"count,omitempty"`
}

// Compacted is the optimized representation of a tool run.
type Compacted struct {
	// Status is "pass", "fail" or "unknown". "unknown" is honest: not every
	// tool announces its result in a parseable way.
	Status string `json:"status"`
	// Summary is a single line describing the outcome.
	Summary string `json:"summary"`
	// Errors and Warnings are the retained diagnostics, most severe first.
	Errors   []Diagnostic `json:"errors,omitempty"`
	Warnings []Diagnostic `json:"warnings,omitempty"`
	// Tail keeps the last few unclassified lines, which is where a tool that
	// DWYT does not recognise usually puts its verdict.
	Tail []string `json:"tail,omitempty"`

	RawRef                    string  `json:"raw_ref,omitempty"`
	RawTokensEst              int     `json:"raw_tokens_est"`
	SentTokensEst             int     `json:"sent_tokens_est"`
	CompressionMetadataTokens int     `json:"compression_metadata_tokens"`
	CompressionPct            float64 `json:"compression_pct"`

	// Suppressed counts what was dropped, by category, so the reduction is
	// auditable rather than magical.
	Suppressed map[string]int `json:"suppressed,omitempty"`
	// Truncated is true when even the compacted form hit the size cap.
	Truncated bool `json:"truncated,omitempty"`
	// PassedThrough is set by ApplyCompressionGate when the net gain did not
	// justify the reduction (Law 6). Callers must send the raw payload (or
	// fetch it via RawRef) instead of the structured envelope.
	PassedThrough bool `json:"passed_through,omitempty"`
}

// Cost model for the compression gate (Law 6): sending the compacted
// envelope also costs its raw-ref handle, and a consumer that needs the
// original bytes later pays a recovery round-trip. Both are real token
// costs and belong in the net-gain math.
const (
	// RecoveryOverheadTokens is the expected cost of a later dwyt_get_raw
	// round-trip amortized over the payload's lifetime.
	RecoveryOverheadTokens = 30
	// DefaultMinGainTokens is the conservative net-gain floor. Calibrated
	// deliberately low: false passthrough wastes a few tokens, false
	// compression loses evidence. Benchmark evidence may tune it (Fase 9).
	DefaultMinGainTokens = 120
)

// EstimateCompressionMetadataTokens returns only the future recovery overhead
// that is not already part of the rendered payload. The raw-ref handle itself
// is included in SentTokensEst after Render, so charging it again would double
// count it in both the gate and net-savings calculation.
func EstimateCompressionMetadataTokens(rawRef string) int {
	if rawRef == "" {
		return 0
	}
	return RecoveryOverheadTokens
}

// ApplyCompressionGate decides whether a compaction result is worth sending
// (Fine-Tuning §3.7: raw cost minus sent payload — which already contains its
// raw-ref handle — minus expected future recovery overhead, pass through when
// the gain is not useful). It is idempotent: applying it twice is a no-op.
// Structured diagnostics survive regardless of gain — Law 7 (critical
// evidence) overrides Law 6.
func ApplyCompressionGate(c Compacted, minGainTokens int) Compacted {
	if c.PassedThrough || c.RawTokensEst <= 0 {
		c.CompressionMetadataTokens = 0
		return c
	}
	metadata := EstimateCompressionMetadataTokens(c.RawRef)
	netGain := c.RawTokensEst - c.SentTokensEst - metadata
	hasEvidence := len(c.Errors) > 0 || len(c.Warnings) > 0
	if netGain >= minGainTokens || hasEvidence {
		c.CompressionMetadataTokens = metadata
		return c
	}
	c.PassedThrough = true
	c.SentTokensEst = c.RawTokensEst
	c.CompressionMetadataTokens = 0
	c.CompressionPct = 0
	c.Errors = nil
	c.Warnings = nil
	c.Suppressed = nil
	c.Tail = nil
	c.Summary = "compression would not reduce cost; passed through"
	return c
}

// Options tunes a compaction pass.
type Options struct {
	// MaxErrors and MaxWarnings cap the retained diagnostics.
	MaxErrors   int
	MaxWarnings int
	// TailLines is how many trailing unclassified lines to keep.
	TailLines int
	// MaxTokens is the soft ceiling for the compacted payload.
	MaxTokens int
	// AlreadyCompact tells the optimizer the producer (RTK) already reduced the
	// output, so a second aggressive pass is skipped (spec §31).
	AlreadyCompact bool
}

// DefaultOptions are the values used when a caller passes the zero Options.
func DefaultOptions() Options {
	return Options{
		MaxErrors:   10,
		MaxWarnings: 5,
		TailLines:   6,
		MaxTokens:   700,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.MaxErrors <= 0 {
		o.MaxErrors = d.MaxErrors
	}
	if o.MaxWarnings <= 0 {
		o.MaxWarnings = d.MaxWarnings
	}
	if o.TailLines <= 0 {
		o.TailLines = d.TailLines
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = d.MaxTokens
	}
	return o
}

// Patterns for the diagnostic formats DWYT actually encounters. Each is
// anchored enough to avoid matching prose, and each is tried in order.
var (
	// tsc / eslint / gcc style: path:line:col: severity CODE: message
	reFileLineCol = regexp.MustCompile(`^\s*([^\s:][^:]*\.[A-Za-z0-9]+):(\d+):(\d+)[:\s-]+(error|warning|fatal error)\b[:\s]*([A-Z]+\d+)?:?\s*(.*)$`)
	// tsc without column: path(line,col): error TS1234: message
	reTSParen = regexp.MustCompile(`^\s*([^\s(][^(]*\.[A-Za-z0-9]+)\((\d+),(\d+)\):\s*(error|warning)\s+([A-Z]+\d+):\s*(.*)$`)
	// Go vet/compiler: path:line:col: message  (no severity keyword)
	reGoDiag = regexp.MustCompile(`^\s*([^\s:][^:]*\.go):(\d+):(\d+):\s*(.+)$`)
	// Go test failure: --- FAIL: TestName (0.00s)
	reGoTestFail = regexp.MustCompile(`^\s*---\s+FAIL:\s+(\S+)`)
	// Generic severity prefix: "error: ...", "ERROR ...", "warning: ..."
	reSeverityPrefix = regexp.MustCompile(`(?i)^\s*(error|fatal|warning|warn)\b[:\s]+(.*)$`)
	// Noise: progress bars, spinners, download meters, carriage-return redraws.
	reProgress = regexp.MustCompile(`^[\s\[\]#=>.\-|/\\%\d]*$`)
	rePercent  = regexp.MustCompile(`\d{1,3}%`)
	// Test/build result summaries worth keeping verbatim.
	reResult = regexp.MustCompile(`(?i)^\s*(ok|FAIL|PASS|no test files|\d+ (passed|failed|skipped)|Tests:|Test Suites:|BUILD (SUCCESSFUL|FAILED)|build (succeeded|failed)|exit (code|status) \d+)`)
	// Common banner/heading noise.
	reBanner = regexp.MustCompile(`(?i)^\s*(> |npm (notice|warn) |yarn run |Downloading |Fetching |Resolving |added \d+ packages|audited \d+ packages|found 0 vulnerabilities|Compiling |Building |\[INFO\] |go: downloading )`)
)

// Compact reduces raw tool output. It never returns an error: a tool output it
// cannot parse degrades to "unknown status plus tail", which is still far
// smaller than the original and still carries the raw reference.
func Compact(raw string, opts Options) Compacted {
	opts = opts.withDefaults()
	out := Compacted{
		Status:       "unknown",
		RawTokensEst: estimateTokens(raw),
		Suppressed:   map[string]int{},
	}
	if strings.TrimSpace(raw) == "" {
		out.Status = "pass"
		out.Summary = "no output"
		out.SentTokensEst = estimateTokens(out.Summary)
		return out
	}

	// Normalize carriage-return redraws: a progress bar rewriting one line
	// arrives as a single huge line separated by \r.
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")

	// The producer (RTK) already reduced this output. Re-running the
	// aggressive pass would re-process text the producer curated, so only the
	// verdict is detected and the size is bounded (spec §31).
	if opts.AlreadyCompact {
		return compactPreReduced(lines, out, opts)
	}

	errorsByKey := map[string]*Diagnostic{}
	warnsByKey := map[string]*Diagnostic{}
	var errorOrder, warnOrder []string
	var results []string
	var unclassified []string
	sawFail := false
	sawPass := false

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			out.Suppressed["blank"]++
			continue
		}
		if isNoise(trimmed) {
			out.Suppressed["noise"]++
			continue
		}
		if reResult.MatchString(trimmed) {
			results = append(results, strings.TrimSpace(trimmed))
			lower := strings.ToLower(trimmed)
			if strings.Contains(lower, "fail") {
				sawFail = true
			}
			if strings.HasPrefix(lower, "ok") || strings.Contains(lower, "pass") ||
				strings.Contains(lower, "successful") || strings.Contains(lower, "succeeded") {
				sawPass = true
			}
			continue
		}

		if d, ok := parseDiagnostic(trimmed); ok {
			key := diagKey(d)
			bucket, order := errorsByKey, &errorOrder
			if d.Severity == SeverityWarning {
				bucket, order = warnsByKey, &warnOrder
			}
			if existing, seen := bucket[key]; seen {
				existing.Count++
				out.Suppressed["duplicate_diagnostic"]++
				continue
			}
			copied := d
			copied.Count = 1
			bucket[key] = &copied
			*order = append(*order, key)
			if d.Severity == SeverityError {
				sawFail = true
			}
			continue
		}

		unclassified = append(unclassified, strings.TrimSpace(trimmed))
	}

	// Progress/redraw sequences arrive as many lines that differ only in their
	// numbers ("downloading 10%", "downloading 50%", ...). A line shape that
	// repeats three or more times is a meter, not content, so the whole group
	// goes. A shape appearing once or twice is kept, which protects a genuine
	// single line like "Coverage: 82.4% of statements".
	unclassified, repeated := dropRepeatedShapes(unclassified, 3)
	if repeated > 0 {
		out.Suppressed["repeated_lines"] += repeated
	}

	for _, key := range errorOrder {
		out.Errors = append(out.Errors, *errorsByKey[key])
	}
	for _, key := range warnOrder {
		out.Warnings = append(out.Warnings, *warnsByKey[key])
	}
	// Most-repeated first: a diagnostic seen 40 times is the one to fix.
	sortDiagnostics(out.Errors)
	sortDiagnostics(out.Warnings)

	if len(out.Errors) > opts.MaxErrors {
		out.Suppressed["errors_over_cap"] = len(out.Errors) - opts.MaxErrors
		out.Errors = out.Errors[:opts.MaxErrors]
		out.Truncated = true
	}
	if len(out.Warnings) > opts.MaxWarnings {
		out.Suppressed["warnings_over_cap"] = len(out.Warnings) - opts.MaxWarnings
		out.Warnings = out.Warnings[:opts.MaxWarnings]
		out.Truncated = true
	}

	// Keep the trailing unclassified lines: tools that DWYT does not know put
	// their verdict at the end.
	if len(unclassified) > opts.TailLines {
		out.Suppressed["tail_lines"] = len(unclassified) - opts.TailLines
		unclassified = unclassified[len(unclassified)-opts.TailLines:]
	}
	out.Tail = append(results, unclassified...)
	if len(out.Tail) > opts.TailLines+len(results) {
		out.Tail = out.Tail[:opts.TailLines+len(results)]
	}

	switch {
	case len(out.Errors) > 0 || sawFail:
		out.Status = "fail"
	case sawPass:
		out.Status = "pass"
	}

	out.Summary = buildSummary(out, len(lines))
	out.SentTokensEst = estimateTokens(out.Render())
	if out.RawTokensEst > 0 {
		saved := out.RawTokensEst - out.SentTokensEst
		if saved < 0 {
			saved = 0
		}
		out.CompressionPct = float64(saved) / float64(out.RawTokensEst) * 100
	}

	// When nothing was extracted and nothing was suppressed, compaction bought
	// nothing: pass the original text through instead of wrapping tiny output
	// in a larger envelope.
	//
	// Note the condition deliberately does *not* include "the render came out
	// bigger". Structured, deduplicated diagnostics are worth keeping even when
	// they are not shorter than four lines of raw text — they are what the
	// agent acts on, and discarding them to win a handful of tokens would
	// trade correctness for size.
	nothingExtracted := len(out.Errors) == 0 && len(out.Warnings) == 0
	nothingSuppressed := totalSuppressed(out.Suppressed) == 0
	if nothingExtracted && nothingSuppressed && out.SentTokensEst >= out.RawTokensEst {
		out.Tail = strings.Split(strings.TrimSpace(normalized), "\n")
		out.Summary = "output already minimal; passed through"
		out.SentTokensEst = estimateTokens(out.Render())
		out.CompressionPct = 0
	}
	return out
}

// compactPreReduced bounds output a producer (RTK) already reduced. The tail
// is what matters — a tool puts its verdict at the end — so lines are kept
// from the end up to the MaxTokens budget (always at least TailLines lines).
// Nothing is reclassified or deduplicated: whatever the producer chose to keep
// is passed through verbatim, and any head that had to go is declared.
func compactPreReduced(lines []string, out Compacted, opts Options) Compacted {
	// A trailing newline splits into a final empty element; it is an artifact,
	// not content, so it never occupies the tail.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	// Detect the verdict from result lines only; RTK output may not carry
	// compiler-style diagnostics, so no structured parsing happens here.
	sawFail := false
	sawPass := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !reResult.MatchString(trimmed) {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "fail") {
			sawFail = true
		}
		if strings.HasPrefix(lower, "ok") || strings.Contains(lower, "pass") ||
			strings.Contains(lower, "successful") || strings.Contains(lower, "succeeded") {
			sawPass = true
		}
	}
	switch {
	case sawFail:
		out.Status = "fail"
	case sawPass:
		out.Status = "pass"
	}

	maxChars := opts.MaxTokens * 4
	minLines := opts.TailLines
	if minLines < 1 {
		minLines = 1
	}
	kept := make([]string, 0, minLines)
	chars := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], " \t")
		if len(kept) >= minLines && chars+len(line) > maxChars {
			dropped := i + 1
			out.Suppressed["head_lines"] = dropped
			out.Truncated = true
			break
		}
		kept = append(kept, line)
		chars += len(line)
	}
	// Reverse back into reading order.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	out.Tail = kept

	out.Summary = "producer output already compact; passed through"
	out.SentTokensEst = estimateTokens(out.Render())
	if out.RawTokensEst > 0 {
		saved := out.RawTokensEst - out.SentTokensEst
		if saved < 0 {
			saved = 0
		}
		out.CompressionPct = float64(saved) / float64(out.RawTokensEst) * 100
	}
	return out
}

// dropRepeatedShapes removes lines whose normalized shape occurs at least
// minRepeats times, returning the surviving lines and how many were dropped.
func dropRepeatedShapes(lines []string, minRepeats int) ([]string, int) {
	if len(lines) < minRepeats {
		return lines, 0
	}
	counts := make(map[string]int, len(lines))
	for _, line := range lines {
		counts[normalizeMessage(line)]++
	}
	out := make([]string, 0, len(lines))
	dropped := 0
	for _, line := range lines {
		if counts[normalizeMessage(line)] >= minRepeats {
			dropped++
			continue
		}
		out = append(out, line)
	}
	return out, dropped
}

func isNoise(line string) bool {
	if reBanner.MatchString(line) {
		return true
	}
	// A line made only of bar/percent characters is a progress meter.
	if reProgress.MatchString(line) && len(strings.TrimSpace(line)) > 0 {
		return true
	}
	if rePercent.MatchString(line) && strings.ContainsAny(line, "=#>.") {
		return true
	}
	return false
}

func parseDiagnostic(line string) (Diagnostic, bool) {
	if m := reTSParen.FindStringSubmatch(line); m != nil {
		return Diagnostic{
			Severity: severityFrom(m[4]),
			File:     m[1],
			Line:     atoi(m[2]),
			Column:   atoi(m[3]),
			Code:     m[5],
			Message:  strings.TrimSpace(m[6]),
		}, true
	}
	if m := reFileLineCol.FindStringSubmatch(line); m != nil {
		return Diagnostic{
			Severity: severityFrom(m[4]),
			File:     m[1],
			Line:     atoi(m[2]),
			Column:   atoi(m[3]),
			Code:     m[5],
			Message:  strings.TrimSpace(m[6]),
		}, true
	}
	if m := reGoTestFail.FindStringSubmatch(line); m != nil {
		return Diagnostic{Severity: SeverityError, Message: "FAIL " + m[1]}, true
	}
	if m := reGoDiag.FindStringSubmatch(line); m != nil {
		return Diagnostic{
			Severity: SeverityError,
			File:     m[1],
			Line:     atoi(m[2]),
			Column:   atoi(m[3]),
			Message:  strings.TrimSpace(m[4]),
		}, true
	}
	if m := reSeverityPrefix.FindStringSubmatch(line); m != nil {
		msg := strings.TrimSpace(m[2])
		if msg == "" {
			return Diagnostic{}, false
		}
		return Diagnostic{Severity: severityFrom(m[1]), Message: msg}, true
	}
	return Diagnostic{}, false
}

func severityFrom(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "warning", "warn":
		return SeverityWarning
	case "error", "fatal", "fatal error":
		return SeverityError
	}
	return SeverityInfo
}

// diagKey is the dedup identity of a diagnostic. Line/column are excluded so
// the same error repeated across many locations collapses per file+code, which
// is what an agent needs to act; the count preserves the magnitude.
func diagKey(d Diagnostic) string {
	return strings.Join([]string{string(d.Severity), d.File, d.Code, normalizeMessage(d.Message)}, "|")
}

// normalizeMessage collapses digits and quoted identifiers so "expected 3, got
// 4" and "expected 7, got 9" dedupe into one entry.
func normalizeMessage(msg string) string {
	var b strings.Builder
	lastDigit := false
	for _, r := range strings.ToLower(msg) {
		if r >= '0' && r <= '9' {
			if !lastDigit {
				b.WriteByte('#')
				lastDigit = true
			}
			continue
		}
		lastDigit = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func sortDiagnostics(items []Diagnostic) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		if items[i].File != items[j].File {
			return items[i].File < items[j].File
		}
		return items[i].Line < items[j].Line
	})
}

func buildSummary(c Compacted, totalLines int) string {
	var parts []string
	if n := totalDiagnostics(c.Errors); n > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s)", n))
	}
	if n := totalDiagnostics(c.Warnings); n > 0 {
		parts = append(parts, fmt.Sprintf("%d warning(s)", n))
	}
	if suppressed := totalSuppressed(c.Suppressed); suppressed > 0 {
		parts = append(parts, fmt.Sprintf("%d line(s) suppressed", suppressed))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s; %d line(s) of output", c.Status, totalLines)
	}
	return strings.Join(parts, "; ")
}

func totalDiagnostics(items []Diagnostic) int {
	total := 0
	for _, d := range items {
		if d.Count > 0 {
			total += d.Count
			continue
		}
		total++
	}
	return total
}

func totalSuppressed(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}

// Render produces the compact text form sent to the model.
func (c Compacted) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "status: %s\n", c.Status)
	if c.Summary != "" {
		fmt.Fprintf(&b, "summary: %s\n", c.Summary)
	}
	renderDiagnostics(&b, "errors", c.Errors)
	renderDiagnostics(&b, "warnings", c.Warnings)
	if len(c.Tail) > 0 {
		b.WriteString("output:\n")
		for _, line := range c.Tail {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	if c.RawRef != "" {
		fmt.Fprintf(&b, "raw_ref: %s\n", c.RawRef)
		fmt.Fprintf(&b, "raw_tokens_est: %d\n", c.RawTokensEst)
	}
	return b.String()
}

func renderDiagnostics(b *strings.Builder, label string, items []Diagnostic) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, d := range items {
		b.WriteString("  - ")
		if d.File != "" {
			b.WriteString(d.File)
			if d.Line > 0 {
				fmt.Fprintf(b, ":%d", d.Line)
				if d.Column > 0 {
					fmt.Fprintf(b, ":%d", d.Column)
				}
			}
			b.WriteString(" ")
		}
		if d.Code != "" {
			b.WriteString(d.Code)
			b.WriteString(" ")
		}
		b.WriteString(d.Message)
		if d.Count > 1 {
			fmt.Fprintf(b, " (x%d)", d.Count)
		}
		b.WriteString("\n")
	}
}

func atoi(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	byChars := (len(content) + 3) / 4
	byWords := len(strings.Fields(content))
	if byWords > byChars {
		return byWords
	}
	return byChars
}
