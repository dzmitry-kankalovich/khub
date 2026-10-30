package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// A preset doc owns its prose; the generator owns only what sits between a
// BEGIN and an END marker of the same region. The regions:
//
//	summary        the version, the counts and the base block
//	relations      the relation vocabulary, one row per distinct declaration
//	types          a "### <type>" section for every type
//	type <name>    one type's storage line, attribute table and relations,
//	               under a heading and prose the author writes
//
// Every doc needs summary and relations, and every type must be covered by
// exactly one of types or its own type region, so a type added to the preset
// without a place in its doc fails the run rather than passing by omission.

var (
	beginRe = regexp.MustCompile(`^<!-- BEGIN GENERATED (.+?) -->$`)
	endRe   = regexp.MustCompile(`^<!-- END GENERATED (.+?) -->$`)
)

// splice rewrites every generated region of doc from s and base. It returns
// every problem it found, not the first, so one run names the whole fix.
func splice(doc string, s *schemaDoc, base *baseDoc) (string, []string) {
	var problems []string
	lines := strings.Split(doc, "\n")
	var out []string
	seen := map[string]int{}
	covered := map[string]int{}
	known := map[string]bool{}
	for _, t := range s.Types {
		known[t.Name] = true
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if m := endRe.FindStringSubmatch(line); m != nil {
			problems = append(problems, fmt.Sprintf("line %d: END GENERATED %s without a BEGIN", i+1, m[1]))
			out = append(out, lines[i])
			continue
		}
		m := beginRe.FindStringSubmatch(line)
		if m == nil {
			out = append(out, lines[i])
			continue
		}
		name := m[1]
		end := -1
		for j := i + 1; j < len(lines); j++ {
			l := strings.TrimRight(lines[j], "\r")
			if beginRe.MatchString(l) {
				break
			}
			if e := endRe.FindStringSubmatch(l); e != nil {
				if e[1] == name {
					end = j
				}
				break
			}
		}
		if end < 0 {
			problems = append(problems, fmt.Sprintf("line %d: BEGIN GENERATED %s has no matching END GENERATED %s", i+1, name, name))
			out = append(out, lines[i])
			continue
		}
		seen[name]++
		body, err := region(name, s, base, covered, known)
		if err != "" {
			problems = append(problems, fmt.Sprintf("line %d: %s", i+1, err))
			out = append(out, lines[i:end+1]...)
		} else {
			out = append(out, line, "", body, "", strings.TrimRight(lines[end], "\r"))
		}
		i = end
	}
	for _, want := range []string{"summary", "relations"} {
		if seen[want] == 0 {
			problems = append(problems, fmt.Sprintf("no BEGIN GENERATED %s region", want))
		}
	}
	for name, n := range seen {
		if n > 1 {
			problems = append(problems, fmt.Sprintf("region %s appears %d times", name, n))
		}
	}
	for _, t := range s.Types {
		switch covered[t.Name] {
		case 0:
			problems = append(problems, fmt.Sprintf("type %s has no generated section: add a BEGIN GENERATED type %s region, or a types region", t.Name, t.Name))
		case 1:
		default:
			problems = append(problems, fmt.Sprintf("type %s is generated %d times", t.Name, covered[t.Name]))
		}
	}
	sort.Strings(problems)
	return strings.Join(out, "\n"), problems
}

func region(name string, s *schemaDoc, base *baseDoc, covered map[string]int, known map[string]bool) (string, string) {
	switch {
	case name == "summary":
		return renderSummary(s, base), ""
	case name == "relations":
		return renderRelations(s, base), ""
	case name == "types":
		parts := make([]string, 0, len(s.Types))
		for _, t := range s.Types {
			covered[t.Name]++
			parts = append(parts, "### "+t.Name+"\n\n"+renderType(t, base))
		}
		return strings.Join(parts, "\n\n"), ""
	case strings.HasPrefix(name, "type "):
		tn := strings.TrimPrefix(name, "type ")
		if !known[tn] {
			return "", fmt.Sprintf("type %s is not in the %s schema", tn, s.Provenance.Preset)
		}
		covered[tn]++
		for _, t := range s.Types {
			if t.Name == tn {
				return renderType(t, base), ""
			}
		}
	}
	return "", fmt.Sprintf("unknown region %q (want summary, relations, types or type <name>)", name)
}

func baseRelation(base *baseDoc, predicate string) (relation, bool) {
	for _, r := range base.Relations {
		if r.Predicate == predicate {
			return r, true
		}
	}
	return relation{}, false
}

func baseField(base *baseDoc, name string) (field, bool) {
	for _, f := range base.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return field{}, false
}

// ownRelations is what a type declares or narrows: every stored relation that
// is not a base edge unchanged, then the computed inverses.
func ownRelations(t typeDef, base *baseDoc) (stored, derived []relation) {
	for _, r := range t.Relations {
		if r.Derived {
			derived = append(derived, r)
			continue
		}
		if b, ok := baseRelation(base, r.Predicate); ok && sameRelation(r, b) {
			continue
		}
		stored = append(stored, r)
	}
	return stored, derived
}

func renderSummary(s *schemaDoc, base *baseDoc) string {
	var declared, inverses []string
	seen := map[string]bool{}
	for _, t := range s.Types {
		for _, r := range t.Relations {
			if seen[r.Predicate] {
				continue
			}
			if _, ok := baseRelation(base, r.Predicate); ok {
				continue
			}
			seen[r.Predicate] = true
			if r.Derived {
				inverses = append(inverses, r.Predicate)
			} else {
				declared = append(declared, r.Predicate)
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Preset version **%s**. %s, and %s: %d declared by the preset and %d from the base block.",
		s.Provenance.Version, plural(len(s.Types), "entity type"),
		plural(len(declared)+len(base.Relations), "relation predicate"), len(declared), len(base.Relations))
	if len(inverses) > 0 {
		fmt.Fprintf(&b, " Computed inverses, never stored: %s.", codeList(inverses))
	}
	b.WriteString("\n\nEvery type also carries the base block, which is embedded in the binary (`khub schema base` prints it):\n\n")
	names := make([]string, len(base.Fields))
	for i, f := range base.Fields {
		names[i] = f.Name
	}
	fmt.Fprintf(&b, "- **Base attributes**: %s.\n", codeList(names))
	edges := make([]string, len(base.Relations))
	for i, r := range base.Relations {
		edges[i] = relationShape(r)
	}
	fmt.Fprintf(&b, "- **Base edges**: %s.", strings.Join(edges, "; "))
	return b.String()
}

func renderRelations(s *schemaDoc, base *baseDoc) string {
	type row struct {
		rel  relation
		from []string
	}
	var rows []*row
	index := map[string]*row{}
	overridden := map[string]bool{}
	for _, t := range s.Types {
		stored, _ := ownRelations(t, base)
		for _, r := range stored {
			if _, ok := baseRelation(base, r.Predicate); ok {
				overridden[r.Predicate] = true
			}
			key := r.signature()
			if index[key] == nil {
				index[key] = &row{rel: r}
				rows = append(rows, index[key])
			}
			index[key].from = append(index[key].from, t.Name)
		}
	}
	for _, r := range base.Relations {
		from := "every type"
		if overridden[r.Predicate] {
			from = "every other type"
		}
		rows = append(rows, &row{rel: r, from: []string{from}})
	}
	var b strings.Builder
	b.WriteString("| Predicate | From | To | Many | Required | Acyclic | Inverse |\n")
	b.WriteString("|---|---|---|---|---|---|---|")
	for _, rw := range rows {
		inv := ""
		if rw.rel.Inverse != nil {
			inv = code(*rw.rel.Inverse)
		}
		fmt.Fprintf(&b, "\n| %s | %s | %s | %s | %s | %s | %s |",
			code(rw.rel.Predicate), strings.Join(rw.from, ", "), strings.Join(rw.rel.To, " \\| "),
			yesNo(rw.rel.Many), yesNo(rw.rel.Required), yesNo(rw.rel.Acyclic), inv)
	}
	return b.String()
}

func renderType(t typeDef, base *baseDoc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Layout `%s`, format `%s`", t.Layout, t.Format)
	switch {
	case len(t.Paths) > 0:
		fmt.Fprintf(&b, ", paths %s", codeList(t.Paths))
	case t.Path != nil:
		fmt.Fprintf(&b, ", path %s", code(*t.Path))
	}
	if t.IDShape != nil {
		fmt.Fprintf(&b, ", ids %s", code(*t.IDShape))
	}
	b.WriteString(".")
	if t.Required {
		b.WriteString(" Required: `check` fails until one exists.")
	}
	if t.Orphan {
		b.WriteString(" Exempt from the orphan sweep.")
	}
	if t.When != nil && strings.TrimSpace(*t.When) != "" {
		fmt.Fprintf(&b, "\n\nRecord one when %s.", strings.TrimRight(strings.Join(strings.Fields(*t.When), " "), "."))
	}

	var rows []string
	for _, f := range t.Fields {
		name := code(f.Name)
		if bf, ok := baseField(base, f.Name); ok {
			if sameField(f, bf) {
				continue
			}
			name += " (base, redeclared)"
		}
		var values []string
		if len(f.Enum) > 0 {
			vs := make([]string, len(f.Enum))
			for i, v := range f.Enum {
				vs[i] = fmt.Sprint(v)
			}
			values = append(values, codeList(vs))
		}
		if f.Pattern != nil {
			values = append(values, "pattern "+code(*f.Pattern))
		}
		def := defaultText(f.Default)
		if def != "" {
			def = code(def)
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s | %s |",
			name, f.Type, yesNo(f.Required), cell(strings.Join(values, "; ")), cell(def)))
	}
	if len(rows) == 0 {
		b.WriteString("\n\nAttributes: only the base block.")
	} else {
		b.WriteString("\n\n| Attribute | Type | Required | Values | Default |\n|---|---|---|---|---|\n")
		b.WriteString(strings.Join(rows, "\n"))
	}

	stored, derived := ownRelations(t, base)
	if len(stored) == 0 {
		b.WriteString("\n\nRelations: only the base edges.")
	} else {
		parts := make([]string, len(stored))
		for i, r := range stored {
			parts[i] = relationShape(r)
		}
		fmt.Fprintf(&b, "\n\nRelations: %s.", strings.Join(parts, "; "))
	}
	if len(derived) > 0 {
		parts := make([]string, len(derived))
		for i, r := range derived {
			parts[i] = relationShape(r)
		}
		fmt.Fprintf(&b, " Computed: %s.", strings.Join(parts, "; "))
	}
	return b.String()
}

// relationShape reads as "`team` → person (many)".
func relationShape(r relation) string {
	var q []string
	if r.Many {
		q = append(q, "many")
	}
	if r.Required {
		q = append(q, "required")
	}
	if r.Acyclic {
		q = append(q, "acyclic")
	}
	if r.Inverse != nil {
		if r.Derived {
			q = append(q, "inverse of "+code(*r.Inverse))
		} else {
			q = append(q, "inverse "+code(*r.Inverse))
		}
	}
	s := code(r.Predicate) + " → " + strings.Join(r.To, " | ")
	if len(q) > 0 {
		s += " (" + strings.Join(q, ", ") + ")"
	}
	return s
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// code wraps s in a code span, widening the fence when s holds a backtick.
func code(s string) string {
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

func codeList(ss []string) string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = code(s)
	}
	return strings.Join(out, ", ")
}

// cell escapes a table cell: a pipe ends a cell even inside a code span.
func cell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
