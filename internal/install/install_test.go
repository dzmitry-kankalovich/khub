package install

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docs are the Markdown files, relative to the repo root, that show install
// commands to people and agents. CHANGELOG.md is history and stays out: an
// entry records the command a release shipped.
var docs = []string{
	"README.md",
	"RELEASING.md",
	"docs/getting-started.md",
	"docs/plugin.md",
	"docs/cli.md",
	"docs/cli-reference.md",
	"plugin/skills/setup/SKILL.md",
	"plugin/skills/khub/SKILL.md",
	".claude/rules/plugin.md",
}

// trigger finds the start of anything that looks like an install command. The
// npm form needs khub in the same code span, so `npm install` alone (a
// project that already pins khub) is not one.
var trigger = regexp.MustCompile(
	"npx skills add\\b" +
		"|(?:/|claude )plugin marketplace (?:add|update)\\b" +
		"|(?:/|claude )plugin (?:install|update)\\b" +
		"|npm (?:install|i)\\b[^`#\n]*khub\\b")

// variants maps each constant to the flags a doc may add after it.
var variants = map[string]*regexp.Regexp{
	NPMInstall:        regexp.MustCompile(`^(?:@X\.Y\.Z|@\d+\.\d+\.\d+)?$`),
	NPMInstallGlobal:  regexp.MustCompile(`^(?:@X\.Y\.Z|@\d+\.\d+\.\d+)?$`),
	SkillsAdd:         regexp.MustCompile(`^(?: (?:-s (?:khub|setup)|-a \S+|-g|-y))*$`),
	MarketplaceAdd:    regexp.MustCompile(`^$`),
	PluginInstall:     regexp.MustCompile(`^(?: --config binary=\S+)?$`),
	MarketplaceUpdate: regexp.MustCompile(`^$`),
	PluginUpdate:      regexp.MustCompile(`^$`),
}

// commands returns every install command in a Markdown text. A paragraph is
// joined into one line first, so a code span that wraps is read whole; a
// fenced block is read line by line.
func commands(text string) []string {
	var units []string
	var para []string
	fenced := false
	flush := func() {
		if len(para) > 0 {
			units = append(units, strings.Join(para, " "))
			para = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			flush()
			fenced = !fenced
		case fenced:
			units = append(units, line)
		case trimmed == "":
			flush()
		default:
			para = append(para, trimmed)
		}
	}
	flush()

	var found []string
	for _, unit := range units {
		for _, loc := range trigger.FindAllStringIndex(unit, -1) {
			rest := unit[loc[1]:]
			if cut := strings.IndexAny(rest, "`#,;|)"); cut >= 0 {
				rest = rest[:cut]
			}
			if cut := strings.Index(rest, "&&"); cut >= 0 {
				rest = rest[:cut]
			}
			cmd := unit[loc[0]:loc[1]] + strings.TrimRight(rest, " .")
			found = append(found, strings.Join(strings.Fields(cmd), " "))
		}
	}
	return found
}

// matches reports whether cmd is one of the constants, with the flags its
// variant allows. A shell `claude plugin …` reads as the `/plugin …` typed at
// the Claude Code prompt.
func matches(cmd string) bool {
	if after, ok := strings.CutPrefix(cmd, "claude plugin "); ok && !strings.HasPrefix(after, "marketplace update ") && !strings.HasPrefix(after, "update ") {
		cmd = "/plugin " + after
	}
	for c, flags := range variants {
		if rest, ok := strings.CutPrefix(cmd, c); ok && flags.MatchString(rest) {
			return true
		}
	}
	return false
}

func TestDocsInstallCommandsMatchTheConstants(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	for _, doc := range docs {
		data, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, cmd := range commands(string(data)) {
			if !matches(cmd) {
				t.Errorf("%s: %q is not an install command internal/install holds", doc, cmd)
				continue
			}
			for c := range variants {
				if strings.HasPrefix(cmd, c) {
					used[c] = true
				}
			}
		}
	}
	// A trigger that stopped matching would pass every doc vacuously.
	for c := range variants {
		if !used[c] {
			t.Errorf("no doc shows %q; the scan or the trigger has drifted", c)
		}
	}
}

func TestCommandsReadsWrappedSpansAndHints(t *testing.T) {
	text := "From a shell the same two steps are `claude plugin marketplace add\n" +
		"endgame-build/khub` and `claude plugin install khub@khub`.\n\n" +
		"```text\n  cd my-hub && npx skills add endgame-build/khub\n" +
		"In Claude Code: /plugin marketplace add endgame-build/khub, then /plugin install khub@khub\n```\n" +
		"If pinned, `npm ci` (or `npm install`) and use `npx @endgame-build/khub`.\n"
	got := commands(text)
	want := []string{
		"claude plugin marketplace add endgame-build/khub",
		"claude plugin install khub@khub",
		"npx skills add endgame-build/khub",
		"/plugin marketplace add endgame-build/khub",
		"/plugin install khub@khub",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands() = %q, want %q", got, want)
	}
	for _, bad := range []string{
		"npx skills add endgame-build/khb",
		"/plugin install khub@khub-marketplace",
		"npm install -D khub",
		"npx skills add endgame-build/khub --all",
	} {
		if matches(bad) {
			t.Errorf("matches(%q) = true, want false", bad)
		}
	}
}
