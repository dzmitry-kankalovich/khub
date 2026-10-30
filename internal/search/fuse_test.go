package search

// Reciprocal rank fusion over several query groups (SearchGroups, fuse):
// the sum, the tie-break, independence from group order, and that one
// distinct group is exactly Search.

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

func hit(type_, slug string, score float64) Hit {
	return Hit{Type: type_, Slug: slug, Title: slug, Score: score, Snippet: slug + " via " + type_}
}

// A hit at rank r adds 1/(60 + r); the sum is rounded to six decimals and
// Groups counts the rankings that held the entity.
func TestFuseSumsReciprocalRanks(t *testing.T) {
	a := []Hit{hit("c", "one", -3), hit("c", "two", -2), hit("c", "three", -1)}
	b := []Hit{hit("c", "three", -5), hit("c", "one", -1)}
	got := fuse([][]Hit{a, b})
	want := map[string]struct {
		fused  float64
		groups int
	}{
		"one":   {math.Round((1.0/61+1.0/62)*1e6) / 1e6, 2},
		"three": {math.Round((1.0/63+1.0/61)*1e6) / 1e6, 2},
		"two":   {math.Round(1.0/62*1e6) / 1e6, 1},
	}
	if len(got) != len(want) {
		t.Fatalf("fused %d hits, want %d: %+v", len(got), len(want), got)
	}
	for _, h := range got {
		w := want[h.Slug]
		if h.Fused != w.fused || h.Groups != w.groups {
			t.Errorf("%s: fused %v groups %d, want %v %d", h.Slug, h.Fused, h.Groups, w.fused, w.groups)
		}
	}
	if s := hitSlugs(got); !reflect.DeepEqual(s, []string{"one", "three", "two"}) {
		t.Errorf("order %v, want best fused first", s)
	}
	if rrfK != 60 {
		t.Errorf("rrfK = %d, want the usual 60", rrfK)
	}
}

// Equal fused scores break on type, then slug, whatever the rankings' own
// order was.
func TestFuseTieBreaksOnTypeSlug(t *testing.T) {
	a := []Hit{hit("z", "b", -9), hit("a", "z", -1)}
	b := []Hit{hit("a", "b", -9), hit("z", "a", -1)}
	got := fuse([][]Hit{a, b})
	var ids []string
	for _, h := range got {
		ids = append(ids, h.Type+"/"+h.Slug)
	}
	if want := []string{"a/b", "z/b", "a/z", "z/a"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("order %v, want %v", ids, want)
	}
}

// The record a fused hit carries is the one from the group that scored it
// best (the lowest BM25), and the fusion does not depend on group order.
func TestFuseOrderIndependentAndKeepsBestRecord(t *testing.T) {
	a := []Hit{hit("c", "one", -3), {Type: "c", Slug: "two", Score: -2, Snippet: "from a"}}
	b := []Hit{{Type: "c", Slug: "two", Score: -4, Snippet: "from b"}, hit("c", "three", -1)}
	ab, ba := fuse([][]Hit{a, b}), fuse([][]Hit{b, a})
	for i := range ab {
		ab[i].group, ba[i].group = 0, 0 // the ranking index is the one thing that may differ
	}
	if !reflect.DeepEqual(ab, ba) {
		t.Errorf("fuse(a, b) != fuse(b, a):\n%+v\n%+v", ab, ba)
	}
	for _, h := range ab {
		if h.Slug == "two" && (h.Score != -4 || h.Snippet != "from b") {
			t.Errorf("two carries %+v, want the record scored -4 from b", h)
		}
	}
	if got := fuse(nil); len(got) != 0 || got == nil {
		t.Errorf("fuse(nil) = %#v, want an empty, non-nil list", got)
	}
}

// One distinct group is Search, bytes and all; a repeated text counts once;
// the order of the groups never shapes the result.
func TestSearchGroupsSingleIsSearch(t *testing.T) {
	ws := sws(t)
	single, err := Search(ws, "modernization", Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, texts := range [][]string{{"modernization"}, {"modernization", "modernization"}} {
		got, err := SearchGroups(ws, texts, Options{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, single) {
			t.Errorf("SearchGroups(%q) differs from Search:\n%+v\n%+v", texts, got, single)
		}
	}
	ab, err := SearchGroups(ws, []string{"modernization", "mainframe"}, Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	ba, err := SearchGroups(ws, []string{"mainframe", "modernization"}, Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ab, ba) {
		t.Errorf("group order changed the result:\n%+v\n%+v", ab, ba)
	}
}

// The fused set is the union of the groups' matches; a hit in both groups
// outranks one in a single group; the limit cuts the fused list and Total
// counts the whole of it.
func TestSearchGroupsFusesAndLimitsAfter(t *testing.T) {
	ws := sws(t)
	// "modernization" hits op-modern and sparse; "mainframe" hits acme only;
	// "invoicing" hits sparse only, so sparse is in two groups.
	res, err := SearchGroups(ws, []string{"modernization", "mainframe", "invoicing"}, Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hitSlugs(res.Hits), []string{"sparse", "acme", "op-modern"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fused order %v, want %v", got, want)
	}
	if res.Total != 3 || res.Searched != 4 {
		t.Errorf("Total %d Searched %d, want 3 and 4", res.Total, res.Searched)
	}
	sparse := res.Hits[0]
	if sparse.Groups != 2 || sparse.Fused != math.Round((1.0/62+1.0/61)*1e6)/1e6 {
		t.Errorf("sparse: groups %d fused %v", sparse.Groups, sparse.Fused)
	}
	if res.Hits[1].Groups != 1 || res.Hits[1].Fused != math.Round(1.0/61*1e6)/1e6 {
		t.Errorf("acme: groups %d fused %v", res.Hits[1].Groups, res.Hits[1].Fused)
	}

	cut, err := SearchGroups(ws, []string{"modernization", "mainframe", "invoicing"}, Options{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := hitSlugs(cut.Hits); !reflect.DeepEqual(got, []string{"sparse"}) || cut.Total != 3 {
		t.Errorf("limit 1: hits %v total %d, want [sparse] and 3", got, cut.Total)
	}
	none, err := SearchGroups(ws, []string{"modernization", "mainframe"}, Options{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Hits) != 0 || none.Total != 3 {
		t.Errorf("limit 0: %d hits total %d, want none and 3", len(none.Hits), none.Total)
	}
}

// --plain applies per group: each group's words are its own terms, and a
// fused hit's match shares judge it against the group its record came from.
func TestSearchGroupsPlainPerGroup(t *testing.T) {
	ws := sws(t)
	res, err := SearchGroups(ws, []string{"modernization estate", "((("}, Options{Limit: 20, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hitSlugs(res.Hits), []string{"acme", "op-modern", "sparse"}; !reflect.DeepEqual(got, want) {
		t.Errorf("plain fused order %v, want %v", got, want)
	}
	for _, h := range res.Hits {
		if h.Groups != 1 || h.Match <= 0 || h.Match > 1 {
			t.Errorf("%s: groups %d match %v", h.Slug, h.Groups, h.Match)
		}
	}
	if res.Capped {
		t.Error("Capped set without a long group")
	}
	// A type filter and an unknown type behave as in Search.
	if _, err := SearchGroups(ws, []string{"a", "b"}, Options{Type: ptr("nope"), Limit: 20}); err == nil {
		t.Error("unknown type accepted")
	}
}

// A malformed group is the same located refusal Search raises, naming that
// group's text.
func TestSearchGroupsMalformedGroup(t *testing.T) {
	ws := sws(t)
	_, err := SearchGroups(ws, []string{"modernization", "((("}, Options{Limit: 20})
	var loc *errs.Located
	if !errors.As(err, &loc) || loc.Code != "bad_search_query" || loc.Target != "(((" {
		t.Fatalf("err = %v, want bad_search_query on '((('", err)
	}
}
