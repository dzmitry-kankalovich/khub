package mdlink

import (
	"reflect"
	"testing"
)

func TestRelativeLinks(t *testing.T) {
	text := "See [a](a.md) and ![img](img/x.png \"title\") and [b](<my notes.md>).\n" +
		"[web](https://example.com/x.md) [mail](mailto:x@y.z) [top](#heading) " +
		"[root](/abs.md) [frag](c.md#part) [q](d.md?x=1) [enc](e%20f.md)\n" +
		"[dup](a.md#again) [paren](g_(1).md) `[code](code.md)` ``[c2](`x`.md)``\n" +
		"[ref]: ../ref.md \"Ref\"\n" +
		"  [angle]: <h i.md>\n" +
		"[^1]: Footnote prose, not a link.\n" +
		"[win](C:/x.md) [empty]() not a link [x] (y.md)\n"
	got := RelativeLinks(text)
	want := []Link{
		{"a.md", "a.md"},
		{"img/x.png", "img/x.png"},
		{"my notes.md", "my notes.md"},
		{"c.md#part", "c.md"},
		{"d.md?x=1", "d.md"},
		{"e%20f.md", "e f.md"},
		{"g_(1).md", "g_(1).md"},
		{"../ref.md", "../ref.md"},
		{"h i.md", "h i.md"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RelativeLinks =\n%v\nwant\n%v", got, want)
	}
}

func TestRelativeLinksNone(t *testing.T) {
	if got := RelativeLinks("plain prose, [brackets] and (parens)\n"); len(got) != 0 {
		t.Errorf("RelativeLinks = %v, want none", got)
	}
}
