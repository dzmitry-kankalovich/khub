package errs

import "testing"

// The message is printed verbatim, so its bytes are pinned here.
func TestPortNotPermittedMessage(t *testing.T) {
	e := PortNotPermitted(80)
	want := "Not permitted to bind port 80; it needs privileges, the system reserves it, " +
		"or another program holds it. Pass --port <n> to serve on another port"
	if e.Code != "port_not_permitted" || e.Error() != want {
		t.Fatalf("PortNotPermitted(80) = %q %q, want port_not_permitted %q", e.Code, e.Error(), want)
	}
}

// Nearest is the DTO's did-you-mean. A wrong guess is worse than silence, so
// the far misses below must stay empty.
func TestNearest(t *testing.T) {
	cases := []struct {
		want       string
		candidates []string
		out        string
	}{
		{"functionl", []string{"functional", "non-functional"}, "functional"},
		{"statsu", []string{"status", "stack"}, "status"},
		{"knd", []string{"type", "kind"}, "kind"},
		{"nope", []string{"type", "tags"}, ""},               // two edits in four is a different word
		{"Functional", []string{"functional"}, "functional"}, // case alone always qualifies
		{"ab", []string{"ac"}, ""},                           // under three characters, case only
		{"AB", []string{"ab"}, "ab"},
		{"kind", []string{"kind", "kinds"}, "kinds"}, // an exact match is never suggested back
		{"agent", []string{"claude", "agents", "both"}, "agents"},
		{"cat", []string{"bat", "cab"}, "bat"}, // ties go to the earlier candidate
		{"x", nil, ""},
	}
	for _, c := range cases {
		if got := Nearest(c.want, c.candidates); got != c.out {
			t.Errorf("Nearest(%q, %v) = %q, want %q", c.want, c.candidates, got, c.out)
		}
	}
}

// The factories fill the DTO fields from what they already know; the message
// is pinned prose and never mentions the suggestion.
func TestEnumViolationDTO(t *testing.T) {
	e := EnumViolation("acepted", "status", []string{"proposed", "accepted"})
	if e.Field != "status" || e.Suggestion != "accepted" || len(e.Expected) != 2 {
		t.Fatalf("EnumViolation DTO = %q %v %q", e.Field, e.Expected, e.Suggestion)
	}
	if e.Error() != "'acepted' is not a valid status (proposed, accepted)" {
		t.Fatalf("EnumViolation message moved: %q", e.Error())
	}
	if l := LookupError("x").WithSuggestion("y"); l.Suggestion != "y" || l.Error() != "No entity 'x' found" {
		t.Fatalf("WithSuggestion = %q %q", l.Suggestion, l.Error())
	}
}
