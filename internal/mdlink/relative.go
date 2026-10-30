package mdlink

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	// A link reference definition: `[label]: dest`, indented up to three
	// spaces, the destination either `<…>` or a run of non-space bytes. A
	// label opening with `^` is a footnote (`[^1]: text`), whose first word is
	// prose, not a destination.
	refDefRE = regexp.MustCompile(`(?m)^ {0,3}\[[^\]\n^][^\]\n]*\]:[ \t]*(<[^>\n]*>|\S+)`)
	// A URI scheme (RFC 3986): `https:`, `mailto:`, `file:`. A Windows drive
	// (`C:`) matches too, and is no relative link either.
	schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

// Link is one relative link destination found in a body: Dest is the
// destination exactly as written, Path the file part a reader follows —
// fragment and query dropped, percent-escapes decoded, `/`-separated.
type Link struct {
	Dest string
	Path string
}

// RelativeLinks lists the relative file links of a Markdown text, one per
// distinct Path (the first destination that names it), in order of first
// appearance: the destinations of inline links and images
// (`[t](dest)`, `![a](dest)`) and of reference definitions (`[l]: dest`).
//
// The caller passes the text with fenced code and HTML comments already
// blanked; code spans are blanked here. A destination with a scheme, one
// starting with `/` (root-relative: relative to what a renderer serves, not
// to anything khub can see), and a bare `#fragment` are not relative file
// links and are left out.
func RelativeLinks(text string) []Link {
	text = maskCodeSpans(text)
	var out []Link
	seen := map[string]bool{}
	add := func(dest string) {
		l, ok := relative(dest)
		if !ok || seen[l.Path] {
			return
		}
		seen[l.Path] = true
		out = append(out, l)
	}
	// Collect with offsets so inline and reference destinations come out in
	// the order they appear in the body.
	type hit struct {
		at   int
		dest string
	}
	var hits []hit
	for i := 0; i+1 < len(text); i++ {
		if text[i] != ']' || text[i+1] != '(' {
			continue
		}
		if dest, ok := inlineDest(text[i+2:]); ok {
			hits = append(hits, hit{i, dest})
		}
	}
	for _, m := range refDefRE.FindAllStringSubmatchIndex(text, -1) {
		hits = append(hits, hit{m[0], text[m[2]:m[3]]})
	}
	// Insertion sort: both lists are already in order, and few.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].at < hits[j-1].at; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	for _, h := range hits {
		add(strings.TrimSuffix(strings.TrimPrefix(h.dest, "<"), ">"))
	}
	return out
}

// inlineDest reads an inline link destination from the text just past `](`:
// `<…>` up to its `>`, or a run up to whitespace or the `)` that balances the
// opening one. ok is false for an empty or unterminated destination.
func inlineDest(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t")
	if strings.HasPrefix(s, "<") {
		end := strings.IndexAny(s, ">\n")
		if end < 0 || s[end] != '>' || end == 1 {
			return "", false
		}
		return s[:end+1], true
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s):
			i++
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 {
				return s[:i], i > 0
			}
			depth--
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			return s[:i], i > 0
		}
	}
	return "", false
}

// relative reports whether dest is a relative file link and, if so, the path
// a reader follows.
func relative(dest string) (Link, bool) {
	if dest == "" || strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "\\") ||
		schemeRE.MatchString(dest) {
		return Link{}, false
	}
	path := dest
	if i := strings.IndexAny(path, "#?"); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		return Link{}, false // `#section`: a link within this document
	}
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	return Link{Dest: dest, Path: path}, true
}

// maskCodeSpans blanks every inline code span (a backtick run, then the
// first later run of the same length), keeping byte length: a link written
// inside one is an example, not a link. An unmatched run is literal text.
func maskCodeSpans(text string) string {
	buf := []byte(text)
	for i := 0; i < len(buf); {
		if buf[i] != '`' {
			i++
			continue
		}
		n := runLen(buf, i)
		closeAt := -1
		for j := i + n; j < len(buf); {
			if buf[j] != '`' {
				j++
				continue
			}
			m := runLen(buf, j)
			if m == n {
				closeAt = j
				break
			}
			j += m
		}
		if closeAt < 0 {
			i += n
			continue
		}
		for k := i; k < closeAt+n; k++ {
			if buf[k] != '\n' {
				buf[k] = ' '
			}
		}
		i = closeAt + n
	}
	return string(buf)
}

func runLen(buf []byte, at int) int {
	n := 0
	for at+n < len(buf) && buf[at+n] == '`' {
		n++
	}
	return n
}
