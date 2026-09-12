package integrate

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
)

var markdownLinkPattern = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
var markdownHeadingPattern = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`)
var firstPartyMCPPattern = regexp.MustCompile("(?m)^\\|\\s*`(dwyt_[a-z_]+)`\\s*\\|")

func repoPath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", ".."}, parts...)...)
}

func docsPath(parts ...string) string {
	return repoPath(append([]string{"docs"}, parts...)...)
}

func documentationFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	if err := filepath.WalkDir(docsPath(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk documentation: %v", err)
	}
	files = append(files, repoPath("readme.md"))
	sort.Strings(files)
	return files
}

func TestDocumentationFirstPartyMCPsAndLaws(t *testing.T) {
	overview, err := os.ReadFile(docsPath("architecture", "overview.md"))
	if err != nil {
		t.Fatalf("read architecture overview: %v", err)
	}

	servers := make(map[string]struct{})
	for _, match := range firstPartyMCPPattern.FindAllStringSubmatch(string(overview), -1) {
		servers[match[1]] = struct{}{}
	}
	wantServers := []string{"dwyt_codebase", "dwyt_obsidian", "dwyt_optimizer"}
	if len(servers) != len(wantServers) {
		t.Fatalf("documented first-party MCP count = %d, want %d (%v)", len(servers), len(wantServers), wantServers)
	}
	for _, server := range wantServers {
		if _, ok := servers[server]; !ok {
			t.Errorf("architecture overview does not document first-party MCP %q", server)
		}
	}

	optimizerLaw, err := os.ReadFile(docsPath("laws", "optimizer-law.md"))
	if err != nil {
		t.Fatalf("read canonical optimizer law: %v", err)
	}
	for _, law := range []string{"Law 1", "Law 7", "Law 13", "Law 14"} {
		if !strings.Contains(string(optimizerLaw), law) {
			t.Errorf("canonical optimizer law is missing %s", law)
		}
	}

	index, err := os.ReadFile(docsPath("readme.md"))
	if err != nil {
		t.Fatalf("read documentation index: %v", err)
	}
	if !strings.Contains(string(index), "laws/optimizer-law.md") {
		t.Fatal("documentation index does not link the Optimizer Law")
	}
	for _, law := range []string{"optimizer-law.md", "codebase-law.md", "obsidian-law.md"} {
		if _, err := os.Stat(docsPath("laws", law)); err != nil {
			t.Errorf("canonical law %q missing: %v", law, err)
		}
	}
}

func TestActiveDocumentationHasNoCompetingToolOrderOrLaws(t *testing.T) {
	forbidden := []string{
		"tools in this priority order",
		"tool priority rule",
		"priority order: rtk",
		"the two main agent laws",
		"two main agent laws",
	}

	for _, path := range documentationFiles(t) {
		rel, err := filepath.Rel(docsPath(), path)
		if err != nil {
			t.Fatal(err)
		}
		// The changelog and the archived pre-v5 plan preserve historical wording;
		// neither is an active policy document.
		if rel == "CHANGELOG.md" || strings.HasPrefix(rel, "rules"+string(filepath.Separator)) {
			continue
		}

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lowerBody := strings.ToLower(string(body))
		for _, phrase := range forbidden {
			if strings.Contains(lowerBody, phrase) {
				t.Errorf("active documentation %s contains obsolete policy phrase %q", path, phrase)
			}
		}
		if strings.Count(string(body), "<!-- DWYT:START -->") > 1 {
			t.Errorf("documentation %s duplicates a DWYT managed block", path)
		}
	}
}

func TestDocumentationPathsAreCaseFoldUnique(t *testing.T) {
	seen := make(map[string]string)
	for _, path := range documentationFiles(t) {
		rel, err := filepath.Rel(docsPath(), path)
		if err != nil {
			t.Fatal(err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		key := strings.ToLower(filepath.ToSlash(rel))
		if prior, ok := seen[key]; ok {
			t.Errorf("documentation paths collide on case-insensitive filesystems: %s and %s", prior, path)
		}
		seen[key] = path
	}
}

func markdownSlug(value string) string {
	var slug strings.Builder
	pendingSeparator := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSeparator && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			slug.WriteRune(r)
			pendingSeparator = false
			continue
		}
		pendingSeparator = true
	}
	return strings.Trim(slug.String(), "-")
}

func markdownAnchors(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read anchor target %s: %v", path, err)
	}

	anchors := make(map[string]struct{})
	seen := make(map[string]int)
	for _, match := range markdownHeadingPattern.FindAllStringSubmatch(string(body), -1) {
		base := markdownSlug(match[1])
		if base == "" {
			continue
		}
		count := seen[base]
		anchor := base
		if count > 0 {
			anchor = fmt.Sprintf("%s-%d", base, count)
		}
		seen[base]++
		anchors[anchor] = struct{}{}
	}
	return anchors
}

func TestDocumentationLinksResolve(t *testing.T) {
	for _, path := range documentationFiles(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, match := range markdownLinkPattern.FindAllStringSubmatch(string(body), -1) {
			target := strings.Trim(strings.TrimSpace(match[1]), "<>")
			if target == "" {
				continue
			}

			parsed, err := url.Parse(target)
			if err != nil {
				t.Errorf("%s has invalid link %q: %v", path, target, err)
				continue
			}
			if parsed.IsAbs() || parsed.Host != "" {
				continue
			}

			resolved := path
			if parsed.Path != "" {
				resolved = filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(parsed.Path)))
			}
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s links to missing local path %q (%s): %v", path, target, resolved, err)
				continue
			}
			if parsed.Fragment == "" {
				continue
			}

			anchor := markdownSlug(parsed.Fragment)
			if _, ok := markdownAnchors(t, resolved)[anchor]; !ok {
				t.Errorf("%s links to missing anchor %q in %s", path, parsed.Fragment, resolved)
			}
		}
	}
}
