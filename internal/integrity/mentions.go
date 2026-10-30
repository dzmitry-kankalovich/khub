// The opt-in mention audit behind `check --mentions`.
//
// A relation is the only thing that feeds the graph; a name written in body
// prose is navigational only. That rule stays. This pass makes the gap it
// leaves visible: an entity whose body names another entity, with no edge
// between the two in either direction, is reported as a suggestion — the
// predicates the mentioning type could use to link it. Nothing is written,
// and the suggestions never affect `passed`.

package integrity

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// Mention is one suggested edge: the body of Type/Slug names Target as
// Text, the two share no edge, and Predicates are the relations of the
// mentioning type that could carry it.
type Mention struct {
	Type       string
	Slug       string
	Target     string // qualified id
	Text       string // the words as the body spells them
	Predicates []string
}

// ID is the mentioning entity's qualified id.
func (m Mention) ID() string { return m.Type + "/" + m.Slug }

// minMentionRunes is the shortest name the audit matches. A one- or
// two-letter name ("QA", "UI") matches ordinary prose far more often than it
// names the entity.
const minMentionRunes = 3

// mentionTerm is one name an entity answers to, keyed for lookup by its
// first word, casefolded.
type mentionTerm struct {
	text   string
	target index.Node
}

// isWordRune is what a name is made of. A hyphen and an underscore join
// words, so a slug (`dana-lee`) is one word and a name never matches the
// middle of a hyphenated compound.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// mentionTermsOf is every name the node answers to: `title`, `name`, each
// alias and the slug, trimmed of surrounding punctuation, deduplicated
// case-insensitively, in that order. A name shorter than minMentionRunes, or
// holding no letter, is dropped.
func mentionTermsOf(node index.Node, meta *omap.Map) []string {
	var raw []string
	for _, key := range []string{"title", "name"} {
		if v, _ := meta.Get(key); values.Truthy(v) {
			raw = append(raw, values.Str(v))
		}
	}
	raw = append(raw, index.AliasesOf(meta)...)
	raw = append(raw, node.Slug)
	seen := map[string]bool{}
	var out []string
	for _, t := range raw {
		t = strings.TrimFunc(t, func(r rune) bool { return !isWordRune(r) })
		if utf8.RuneCountInString(t) < minMentionRunes || !strings.ContainsFunc(t, unicode.IsLetter) {
			continue
		}
		k := strings.ToLower(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out
}

// firstWord is the leading run of word runes, lowercased.
func firstWord(s string) string {
	end := strings.IndexFunc(s, func(r rune) bool { return !isWordRune(r) })
	if end < 0 {
		end = len(s)
	}
	return strings.ToLower(s[:end])
}

// mentionSpan is one match of a term in a body.
type mentionSpan struct {
	start, end int
	target     index.Node
}

// findMentions returns the matches in body, left to right. A match starts and
// ends on a word boundary and compares case-insensitively. Where matches
// overlap, the one starting first wins, then the longest: "Initech POV" names
// the opportunity, not the client called "Initech". A name two entities share
// matches both.
func findMentions(body string, terms map[string][]mentionTerm) []mentionSpan {
	var all []mentionSpan
	prevWord := false
	for i, r := range body {
		word := isWordRune(r)
		if word && !prevWord {
			for _, t := range terms[firstWord(body[i:])] {
				end := i + len(t.text)
				if end > len(body) || !strings.EqualFold(body[i:end], t.text) {
					continue
				}
				if next, _ := utf8.DecodeRuneInString(body[end:]); end < len(body) && isWordRune(next) {
					continue
				}
				all = append(all, mentionSpan{i, end, t.target})
			}
		}
		prevWord = word
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].start != all[j].start {
			return all[i].start < all[j].start
		}
		return all[i].end > all[j].end
	})
	var out []mentionSpan
	cursor := 0
	var last mentionSpan
	for _, m := range all {
		switch {
		case len(out) > 0 && m.start == last.start && m.end == last.end:
			out = append(out, m)
		case m.start >= cursor:
			out = append(out, m)
			last, cursor = m, m.end
		}
	}
	return out
}

// mentionPredicates is the relations of rtype that could link to the target
// type: those naming it first, then the `to: any` ones, each in declaration
// order. A single-valued relation the entity already fills is left out —
// linking it would replace an edge, not add one.
func mentionPredicates(rtype *schema.ResolvedType, meta *omap.Map, target string) []string {
	var named, anyKind []string
	for _, predicate := range rtype.Relations.Keys() {
		rel, _ := rtype.Relations.Get(predicate)
		if !rel.Many && present(metaGet(meta, predicate)) {
			continue
		}
		if rel.Kind == schema.KindAny {
			anyKind = append(anyKind, predicate)
			continue
		}
		for _, t := range rel.Targets {
			if t == target {
				named = append(named, predicate)
				break
			}
		}
	}
	return append(named, anyKind...)
}

// mentionSuggestions is the audit: for every entity with a body, each other
// entity the body names that shares no edge with it, and that some relation
// of the mentioning type could link. Rows are in (type, slug) order of the
// mentioning entity, then target id order; each pair is one row, carrying the
// first spelling the body used. idx must carry bodies.
func mentionSuggestions(resolved *schema.ResolvedSchema, idx *index.Index, g *graph.Graph, nodes []index.Node) []Mention {
	terms := map[string][]mentionTerm{}
	for _, n := range nodes {
		for _, t := range mentionTermsOf(n, idx.Meta[n]) {
			k := firstWord(t)
			terms[k] = append(terms[k], mentionTerm{text: t, target: n})
		}
	}
	out := []Mention{}
	for _, source := range nodes {
		body := idx.Body[source]
		if body == "" {
			continue
		}
		rtype, _ := resolved.Types.Get(source.Type)
		first := map[index.Node]string{}
		var targets []index.Node
		for _, m := range findMentions(body, terms) {
			if m.target == source || g.Linked(source, m.target) {
				continue
			}
			if _, seen := first[m.target]; !seen {
				first[m.target] = body[m.start:m.end]
				targets = append(targets, m.target)
			}
		}
		index.SortNodes(targets)
		for _, t := range targets {
			predicates := mentionPredicates(rtype, idx.Meta[source], t.Type)
			if len(predicates) == 0 {
				continue
			}
			out = append(out, Mention{
				Type: source.Type, Slug: source.Slug,
				Target: t.ID(), Text: first[t], Predicates: predicates,
			})
		}
	}
	return out
}
