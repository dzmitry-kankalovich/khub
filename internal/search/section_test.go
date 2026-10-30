package search

import (
	"reflect"
	"strings"
	"testing"
)

// headingPath reads ATX headings only, skips fenced code, and pops deeper
// headings when a shallower one opens.
func TestHeadingPath(t *testing.T) {
	text := strings.Join([]string{
		"Preamble.",            // 0
		"# Design",             // 1
		"## Write path ##",     // 2
		"~~~~",                 // 3
		"# not a heading",      // 4
		"~~~",                  // 5 too short to close
		"~~~~~",                // 6 closes
		"### Locking",          // 7
		"    # indented code",  // 8
		"#nospace",             // 9
		"## Read path",         // 10
		"####### seven hashes", // 11
		"   ###   Deep   #",    // 12
		"Tail.",                // 13
	}, "\n")
	lineStart := func(n int) int {
		pos := 0
		for i := 0; i < n; i++ {
			pos += strings.IndexByte(text[pos:], '\n') + 1
		}
		return pos
	}
	cases := []struct {
		line int
		want []string
	}{
		{0, nil},
		{1, []string{"Design"}},
		{4, []string{"Design", "Write path"}},
		{6, []string{"Design", "Write path"}},
		{9, []string{"Design", "Write path", "Locking"}},
		{11, []string{"Design", "Read path"}},
		{13, []string{"Design", "Read path", "Deep"}},
	}
	for _, c := range cases {
		if got := headingPath(text, lineStart(c.line)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("line %d: got %q, want %q", c.line, got, c.want)
		}
	}
}

// The section follows the snippet: a body hit names its heading path, a
// title hit, a frontmatter value and text before the first heading name
// none.
func TestSearchSection(t *testing.T) {
	root := freshWS(t)
	seedRaw(t, root, "clients/store.md",
		"---\ntype: client\nname: Store\nsummary: zebra\ncreated: 2026-06-01\n---\n"+
			"Intro.\n\n# Design\n\n## Locking\n\nWe flock the lockfile.\n")
	for _, c := range []struct {
		text string
		want []string
	}{
		{"lockfile", []string{"Design", "Locking"}},
		{"store", nil},
		{"intro", nil},
		{"zebra", nil},
	} {
		res, err := Search(root, c.text, Options{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Hits) != 1 || !reflect.DeepEqual(res.Hits[0].Section, c.want) {
			t.Errorf("%s: got %+v, want section %q", c.text, res.Hits, c.want)
		}
	}
}
