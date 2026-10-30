package backfill

// --infer-type: a stray whose frontmatter lacks `type` is stamped from the
// layout that holds it; every other stray is left and reported.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
)

// strayWS holds one stray of each shape: stampable in a file and a folder
// layout, fenceless, another type, and a null type (stampable).
func strayWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	writeRaw(t, root, "clients/acme.md", "---\nname: Acme  # kept\n---\nBody stays.\n")
	writeRaw(t, root, "opportunities/big-deal/_index.md", "---\nname: Big Deal\n---\n")
	writeRaw(t, root, "fragments/null-type.md", "---\ntype:\nstage: raw\n---\n")
	writeRaw(t, root, "fragments/prose.md", "just prose\n")
	writeRaw(t, root, "fragments/wrong.md", "---\ntype: client\nname: Wrong\n---\n")
	return root
}

func TestInferTypeStampsOnlyMissingType(t *testing.T) {
	root := strayWS(t)
	report := mustBackfillOpts(t, root, nil, true, false)

	if got := readRaw(t, root, "clients/acme.md"); got != "---\nname: Acme  # kept\ntype: client\n---\nBody stays.\n" {
		t.Errorf("clients/acme.md = %q", got)
	}
	if got := field(t, root, "opportunities/big-deal/_index.md", "type"); got != "opportunity" {
		t.Errorf("folder stray type = %v, want opportunity", got)
	}
	if got := field(t, root, "fragments/null-type.md", "type"); got != "fragment" {
		t.Errorf("null type = %v, want fragment", got)
	}
	if got := readRaw(t, root, "fragments/prose.md"); got != "just prose\n" {
		t.Errorf("fenceless stray was rewritten: %q", got)
	}
	if got := field(t, root, "fragments/wrong.md", "type"); got != "client" {
		t.Errorf("a disagreeing type was overwritten: %v", got)
	}
	if got := report.StampedEntities(); got != 3 {
		t.Errorf("stamped = %d, want 3", got)
	}
	want := []Stray{
		{Path: "fragments/prose.md", Reason: "no frontmatter to stamp"},
		{Path: "fragments/wrong.md", Reason: "type is 'client', the layout holds 'fragment'"},
	}
	if !reflect.DeepEqual(report.Strays, want) {
		t.Errorf("strays = %+v, want %+v", report.Strays, want)
	}

	// The stamped files are entities now; check reports only what was left.
	rep, err := integrity.Check(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"fragments/prose.md", "fragments/wrong.md"}; !reflect.DeepEqual(rep.Strays, want) {
		t.Errorf("check strays = %v, want %v", rep.Strays, want)
	}
}

func TestInferTypeDryRunWritesNothing(t *testing.T) {
	root := strayWS(t)
	before := readRaw(t, root, "clients/acme.md")
	report := mustBackfillOpts(t, root, nil, true, true)
	if got := readRaw(t, root, "clients/acme.md"); got != before {
		t.Errorf("dry run wrote: %q", got)
	}
	var stamped []string
	for _, c := range report.Changes {
		if c.Source == SourceLayout {
			stamped = append(stamped, c.ID()+"="+c.Value)
		}
	}
	want := []string{"client/acme=client", "fragment/null-type=fragment", "opportunity/big-deal=opportunity"}
	if !reflect.DeepEqual(stamped, want) {
		t.Errorf("stamps = %v, want %v", stamped, want)
	}
}

// Without the flag nothing about strays changes: no stamp, no report.
func TestBackfillWithoutInferTypeLeavesStrays(t *testing.T) {
	root := strayWS(t)
	report := mustBackfill(t, root, nil, false)
	if report.InferType || len(report.Strays) != 0 || report.StampedEntities() != 0 {
		t.Errorf("report = %+v", report)
	}
	if got := readRaw(t, root, "clients/acme.md"); strings.Contains(got, "type:") {
		t.Errorf("stamped without --infer-type: %q", got)
	}
}

// A stamped stray gets its git dates in the same run, as any entity would.
func TestInferTypeThenDates(t *testing.T) {
	root := freshWS(t)
	writeRaw(t, root, "clients/acme.md", "---\nname: Acme\n---\n")
	gitInit(t, root)
	commit(t, root, "seed", "2026-01-01T12:00:00")
	mustBackfillOpts(t, root, nil, true, false)
	meta := metaOf(t, root, "clients/acme.md")
	if got := strings.Join(meta.Keys(), ","); got != "name,type,created,updated" {
		t.Errorf("keys = %s", got)
	}
}

// Ambiguity refuses. The resolver already rejects overlapping storage paths, so
// no scan can produce two claimants today; the guard stays for the day it can.
func TestInferTypeRefusesTwoClaimants(t *testing.T) {
	root := freshWS(t)
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	rtype, _ := resolved.Types.Get("client")
	reason, err := strayReason(root, "clients/acme.md", rtype, omap.New(), []string{"client", "person"})
	if err != nil {
		t.Fatal(err)
	}
	if reason != "claimed by more than one type (client, person)" {
		t.Errorf("reason = %q", reason)
	}
}
