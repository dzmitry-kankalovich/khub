package integrity

import (
	"testing"

	"github.com/endgame-build/khub/internal/entity"
)

// Findings must name exactly what fails the gate: one finding per gating
// bucket entry, none for a report that passes. A bucket added to Passed and
// forgotten here fails the first case list; an informational bucket leaking
// into the warnings fails the second.
func TestFindingsFollowPassed(t *testing.T) {
	gating := map[string]*CheckReport{
		"incomplete":                {Incomplete: []Incomplete{{Type: "project", Slug: "p", MissingRelations: []string{"owner"}}}},
		"orphans":                   {Strict: true, Orphans: []string{"client/c"}},
		"dangling":                  {Dangling: []Dangling{{Type: "project", Slug: "p", Predicate: "client", Target: "gone"}}},
		"strays":                    {Strays: []string{"clients/x.txt"}},
		"stray_templates":           {StrayTemplates: []string{".khub/templates/old.yaml"}},
		"missing_templates":         {MissingTemplates: []string{"project: .khub/templates/gone.yaml"}},
		"template_invalid":          {TemplateInvalid: []FieldError{{Type: "project", Slug: "*", Reason: "bad"}}},
		"body_shape":                {BodyShape: []FieldError{{Type: "project", Slug: "p", Reason: "missing heading"}}},
		"misplaced":                 {Misplaced: []Misplaced{{Path: "lost.md", Type: "client", Expected: "clients/"}}},
		"malformed":                 {Malformed: []string{"clients/broken.md"}},
		"cycles":                    {Cycles: [][]string{{"a/x", "a/y", "a/x"}}},
		"missing_singletons":        {MissingSingletons: []string{"prd"}},
		"draft_required_singletons": {DraftRequiredSingletons: []string{"prd"}, DraftSingletons: []string{"prd"}},
		"alias_conflicts":           {AliasConflicts: []AliasConflict{{Alias: "x", Claimants: []string{"a/x", "b/x"}}}},
	}
	for rule, report := range gating {
		if report.Passed() {
			t.Errorf("%s: the fixture report passes; it should fail the gate", rule)
		}
		got := report.Findings()
		if len(got) != 1 || got[0].Rule != rule {
			t.Errorf("%s: findings = %+v, want one under that rule", rule, got)
		}
	}
	informational := map[string]*CheckReport{
		"orphans outside strict":   {Orphans: []string{"client/c"}},
		"thin":                     {Thin: []FieldError{{Type: "project", Slug: "p", Reason: "short"}}},
		"optional draft singleton": {DraftSingletons: []string{"arc42"}},
		"suppressed dangling":      {SuppressedDangling: 3},
	}
	for name, report := range informational {
		if !report.Passed() {
			t.Errorf("%s: the fixture report fails; it should pass", name)
		}
		if got := report.Findings(); len(got) != 0 {
			t.Errorf("%s: findings = %+v, want none", name, got)
		}
	}
}

func TestFindingLine(t *testing.T) {
	report := &CheckReport{Dangling: []Dangling{{Type: "project", Slug: "p", Predicate: "client", Target: "gone"}}}
	if got, want := report.Findings()[0].Line(), "project/p › dangling: client -> 'gone' does not resolve"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func sessionLines(t *testing.T, root, session string) []string {
	t.Helper()
	found, err := SessionWarnings(root, session)
	if err != nil {
		t.Fatalf("SessionWarnings(%s): %v", session, err)
	}
	lines := []string{}
	for _, f := range found {
		lines = append(lines, f.Line())
	}
	return lines
}

// Each session sees standing debt once; a fixed finding that comes back is
// new again.
func TestSessionWarnings(t *testing.T) {
	root := cleanWS(t)
	if got := sessionLines(t, root, "s1"); len(got) != 0 {
		t.Fatalf("clean workspace warned: %v", got)
	}
	create(t, root, "project", "half-pov", "title", "Half", "client", "initech")
	want := "project/half-pov › incomplete: missing owner"

	if got := sessionLines(t, root, "s1"); len(got) != 1 || got[0] != want {
		t.Fatalf("s1 first sight = %v, want [%s]", got, want)
	}
	if got := sessionLines(t, root, "s1"); len(got) != 0 {
		t.Fatalf("s1 repeated a finding it was shown: %v", got)
	}
	if got := sessionLines(t, root, "s2"); len(got) != 1 || got[0] != want {
		t.Fatalf("s2 did not see the standing debt: %v", got)
	}

	link(t, root, "half-pov", "owner", "noor")
	if got := sessionLines(t, root, "s1"); len(got) != 0 {
		t.Fatalf("a fix warned: %v", got)
	}
	if _, err := entity.Unlink(root, "half-pov", "owner", "noor"); err != nil {
		t.Fatal(err)
	}
	if got := sessionLines(t, root, "s1"); len(got) != 1 || got[0] != want {
		t.Fatalf("a reintroduced finding = %v, want [%s]", got, want)
	}

	// The session state lives under .khub/generated/, which no scan reads.
	if report := mustCheck(t, root, false); len(report.Strays) != 0 || len(report.Misplaced) != 0 {
		t.Fatalf("session state surfaced in check: %+v", report)
	}
}
