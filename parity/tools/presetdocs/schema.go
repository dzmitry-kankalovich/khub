package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The slice of `khub schema --format json` and `khub schema base --format json`
// this tool reads. Decoding through encoding/json is fine here: the JSON
// choke point governs the bytes khub writes, and nothing below writes JSON.

type schemaDoc struct {
	Provenance struct {
		Preset  string `json:"preset"`
		Version string `json:"version"`
	} `json:"provenance"`
	Types []typeDef `json:"types"`
}

type baseDoc struct {
	Fields    []field    `json:"fields"`
	Relations []relation `json:"relations"`
}

type typeDef struct {
	Name      string     `json:"name"`
	Layout    string     `json:"layout"`
	Format    string     `json:"format"`
	Path      *string    `json:"path"`
	Paths     []string   `json:"paths"`
	IDShape   *string    `json:"id_shape"`
	Required  bool       `json:"required"`
	Orphan    bool       `json:"orphan"`
	When      *string    `json:"when"`
	Fields    []field    `json:"fields"`
	Relations []relation `json:"relations"`
}

type field struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Required bool            `json:"required"`
	Enum     []any           `json:"enum"`
	Pattern  *string         `json:"pattern"`
	Default  json.RawMessage `json:"default"`
}

type relation struct {
	Predicate string   `json:"predicate"`
	To        []string `json:"to"`
	Kind      string   `json:"kind"`
	Many      bool     `json:"many"`
	Required  bool     `json:"required"`
	Inverse   *string  `json:"inverse"`
	Acyclic   bool     `json:"acyclic"`
	Derived   bool     `json:"derived"`
}

func decode(data []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(into)
}

// sameField reports whether a type's field is the base field unchanged.
func sameField(a, b field) bool {
	return a.Name == b.Name && a.Type == b.Type && a.Required == b.Required &&
		fmt.Sprint(a.Enum) == fmt.Sprint(b.Enum) && strp(a.Pattern) == strp(b.Pattern) &&
		defaultText(a.Default) == defaultText(b.Default)
}

// sameRelation reports whether a type's relation is the base edge unchanged.
func sameRelation(a, b relation) bool {
	return a.signature() == b.signature()
}

func (r relation) signature() string {
	return fmt.Sprintf("%s|%q|%s|%t|%t|%s|%t|%t",
		r.Predicate, r.To, r.Kind, r.Many, r.Required, strp(r.Inverse), r.Acyclic, r.Derived)
}

func strp(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// defaultText renders a field default: a string as itself, anything else as
// its JSON spelling; "" when there is none.
func defaultText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var v any
	if err := decode(raw, &v); err == nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return string(raw)
}
