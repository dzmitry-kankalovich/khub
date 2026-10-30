package query

// count.go — cardinality filters (#58): keep an entity by how many edges it
// has, not only whether it has one. `--count <name><op><n>` counts the
// resolved edges answering to one relation or declared inverse;
// `--in-count`/`--out-count <op><n>` count every inbound or outbound edge.
//
// The counts read the same derived graph `--has`, `--orphan` and `check` read,
// so `--count x>=1` keeps exactly what `--has x` keeps, `--count x=0` exactly
// what `--missing x` keeps, and `--in-count 0 --out-count 0` exactly the edge-less
// entities `--orphan` reports before a type's `orphan: true` exemption.

import (
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
)

// Comparison is one `<op><n>` bound on a count.
type Comparison struct {
	Op string // one of =, !=, <, <=, >, >=
	N  int
}

// Count bounds the edge count of one relation or declared inverse.
type Count struct {
	Name string
	Cmp  Comparison
}

// ops lists the two-character operators first, so `>=` never reads as `>`
// followed by a value of `=2`.
var ops = []string{">=", "<=", "!=", "=", "<", ">"}

// ParseComparison reads `<op><n>`, or a bare `<n>` meaning `=<n>`. n is a
// non-negative decimal integer. ok is false on anything else.
func ParseComparison(expr string) (Comparison, bool) {
	op := "="
	rest := expr
	for _, candidate := range ops {
		if strings.HasPrefix(expr, candidate) {
			op, rest = candidate, expr[len(candidate):]
			break
		}
	}
	if rest == "" {
		return Comparison{}, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return Comparison{}, false // no sign, no space, no fraction
		}
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return Comparison{}, false // out of range
	}
	return Comparison{Op: op, N: n}, true
}

// ParseCount reads `<name><op><n>`. The operator is required here: it is what
// separates the name from the number.
func ParseCount(expr string) (Count, bool) {
	i := strings.IndexAny(expr, "=!<>")
	if i <= 0 {
		return Count{}, false
	}
	cmp, ok := ParseComparison(expr[i:])
	if !ok {
		return Count{}, false
	}
	return Count{Name: expr[:i], Cmp: cmp}, true
}

// Holds reports whether n satisfies the comparison.
func (c Comparison) Holds(n int) bool {
	switch c.Op {
	case "!=":
		return n != c.N
	case "<":
		return n < c.N
	case "<=":
		return n <= c.N
	case ">":
		return n > c.N
	case ">=":
		return n >= c.N
	default:
		return n == c.N
	}
}

// edgeCount counts node's resolved edges answering to name: the stored
// forward edges under that predicate plus, for a declared inverse, the inbound
// edges from the relations that declare it. It is zero exactly when hasEdge is
// false. The graph keeps one edge per (source, target, predicate) and drops
// self-references, so this is the number of distinct other entities on that
// edge.
func edgeCount(g *graph.Graph, node index.Node, name string, inverseOf map[inverseSource]bool) int {
	n := 0
	for _, e := range g.OutEdges(node) {
		if e.Predicate == name {
			n++
		}
	}
	if len(inverseOf) == 0 {
		return n
	}
	for _, e := range g.InEdges(node) {
		if inverseOf[inverseSource{e.From.Type, e.Predicate}] {
			n++
		}
	}
	return n
}
