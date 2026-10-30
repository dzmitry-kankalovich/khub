package schema

// The layer JSON Schema against the vocabulary walk it describes: the committed
// copy is what LayerJSONSchema builds, every shipped preset layer passes it,
// every key list the walk forbids extras with is a property set of it, and on
// a table of probes it never rejects what the walk accepts.

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

var updateJSONSchema = flag.Bool("update-jsonschema", false,
	"rewrite schemas/layers.schema.json from LayerJSONSchema")

const jsonSchemaFile = "../../schemas/layers.schema.json"

func TestLayerJSONSchemaFileIsCurrent(t *testing.T) {
	want, err := canon.EncodeDisk(LayerJSONSchema())
	if err != nil {
		t.Fatal(err)
	}
	if *updateJSONSchema {
		if err := os.WriteFile(jsonSchemaFile, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(jsonSchemaFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s is stale; regenerate it with\n"+
			"  go test ./internal/schema -run TestLayerJSONSchemaFileIsCurrent -update-jsonschema",
			jsonSchemaFile)
	}
}

// committedSchema decodes the committed file, so the checks below hold for the
// bytes an editor reads.
func committedSchema(t *testing.T) *omap.Map {
	t.Helper()
	raw, err := os.ReadFile(jsonSchemaFile)
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.DecodeOrderedJSONStrict(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*omap.Map)
}

// TestLayerJSONSchemaCoversVocabulary pins each closed mapping of the schema to
// the key list the walk forbids extras with. LayerJSONSchema already panics on
// a mismatch; this names which list moved.
func TestLayerJSONSchemaCoversVocabulary(t *testing.T) {
	s := committedSchema(t)
	cases := []struct {
		path []string
		keys []string
	}{
		{[]string{}, layerDocKeys},
		{[]string{"properties", "ontology", "anyOf", "0"}, authoredOntologyKeys},
		{[]string{"definitions", "entity"}, ontologyTypeKeys},
		{[]string{"definitions", "attribute"}, attributeKeys},
		{[]string{"definitions", "relation"}, relationKeys},
		{[]string{"definitions", "policy"}, policyTypeKeys},
		{[]string{"definitions", "storage"}, storageTypeKeys},
		{[]string{"definitions", "id_prefix_by"}, idPrefixDeclKeys},
	}
	for _, tc := range cases {
		node := dig(t, s, tc.path)
		props, _ := node.Get("properties")
		got := slices.Clone(props.(*omap.Map).Keys())
		if !slices.Equal(got, tc.keys) {
			t.Errorf("%s: properties %v, vocabulary %v", strings.Join(tc.path, "."), got, tc.keys)
		}
		if ap, _ := node.Get("additionalProperties"); ap != false {
			t.Errorf("%s: additionalProperties = %v, want false (the walk forbids extras)",
				strings.Join(tc.path, "."), ap)
		}
	}
	// The walk's own ontology list differs from the authored one by `base`
	// alone, which mergeLayers rejects from an authored file.
	extra := []string{}
	for _, k := range ontologyKeys {
		if !slices.Contains(authoredOntologyKeys, k) {
			extra = append(extra, k)
		}
	}
	if !slices.Equal(extra, []string{"base"}) {
		t.Errorf("ontologyKeys beyond the authored ones = %v, want [base]", extra)
	}
}

// TestVocabWalkUsesTheKeyLists holds every addExtras call in vocab.go to a
// named key list, so a key cannot reach the walk without reaching the schema.
func TestVocabWalkUsesTheKeyLists(t *testing.T) {
	src, err := os.ReadFile("vocab.go")
	if err != nil {
		t.Fatal(err)
	}
	calls := regexp.MustCompile(`(?m)^\s*addExtras\((.*)\)\s*$`).FindAllStringSubmatch(string(src), -1)
	if len(calls) == 0 {
		t.Fatal("found no addExtras calls in vocab.go")
	}
	tail := regexp.MustCompile(`, [a-zA-Z]+Keys\.\.\.$`)
	for _, c := range calls {
		if !tail.MatchString(c[1]) {
			t.Errorf("addExtras(%s) lists its keys inline; name them in vocab.go's key lists", c[1])
		}
	}
}

func TestShippedPresetLayersPassTheJSONSchema(t *testing.T) {
	s := committedSchema(t)
	presets := presetsDir(t)
	entries, err := os.ReadDir(presets)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "core" {
			continue
		}
		for _, p := range presetPaths(t, e.Name()) {
			doc := parseLayerFile(t, p)
			if errs := jsValidate(s, s, doc, "$"); len(errs) > 0 {
				t.Errorf("%s fails the JSON Schema:\n  %s", p, strings.Join(errs, "\n  "))
			}
			checked++
		}
	}
	if checked < 6 {
		t.Fatalf("checked %d preset layer files; want every layer of build-hub and firm-ops", checked)
	}
	// The embedded base is not an authoring surface: an authored ontology.base
	// is refused, and the schema says so too.
	core := parseLayerFile(t, filepath.Join(presets, "core", "ontology.yaml"))
	if errs := jsValidate(s, s, core, "$"); len(errs) == 0 {
		t.Error("core/ontology.yaml passes the JSON Schema; an authored ontology.base must not")
	}
}

// TestJSONSchemaAgreesWithTheWalk runs probes through both. A probe the walk
// accepts must pass the schema (the schema is never stricter, falsy layer
// blocks aside); a probe the walk rejects on a single-field rule must fail it.
func TestJSONSchemaAgreesWithTheWalk(t *testing.T) {
	s := committedSchema(t)
	const ont = "ontology:\n  entities:\n    a:\n      attributes:\n        kind: { enum: [x, y] }\n    b: {}\n"
	accepted := []string{
		"",
		"version: '1.0'\n",
		"ontology:\npolicy:\nstorage:\n",
		"ontology:\n  entities:\n",
		ont,
		ont + "policy:\n  a: { required: 'No', orphan: 'On', path_refs: 0 }\n  b: { required: false, orphan: F, path_refs: y }\n",
		ont + "storage:\n  a: { layout: file }\n",
		ont + "storage:\n  a: { layout: file, path: null, format: md, id_prefix: null, template: null }\n",
		ont + "storage:\n  a: { layout: file, path: 'w/{slug}.md' }\n",
		ont + "storage:\n  a: { layout: file, path: ['w/{slug}.md', 'v/{slug}.md'] }\n",
		ont + "storage:\n  a: { layout: collection, format: jsonl }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: ad, id_date: TRUE, id_ordinal: n }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: { by: kind, map: { x: xa, y: ya } } }\n",
		ont + "storage:\n  a: { layout: file, template: false }\n",
		ont + "storage:\n  a: { layout: file, template: note-body }\n",
		"ontology:\n  entities:\n    a:\n      when: null\n      attributes:\n" +
			"        f: { type: null, required: null, default: null, enum: null, pattern: null }\n" +
			"        g: { type: date, default: 2026-01-01, required: 'off' }\n" +
			"        h: {}\n      relations:\n" +
			"        r: { to: any, many: 'yes', required: 0, inverse: null, acyclic: true }\n" +
			"        s: { to: [a, b] }\n",
	}
	rejected := []string{
		"nope: 1\n",
		"ontology:\n  base: {}\n",
		"ontology:\n  entitles: {}\n",
		"ontology:\n  entities:\n    a: { nope: 1 }\n",
		"ontology:\n  entities:\n    a: { when: 3 }\n",
		"ontology:\n  entities:\n    a: { attributes: null }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { type: string }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { enum: x }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { enum: [1] }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { required: maybe }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { nope: 1 }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { many: true }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { to: 42 }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { to: [a, 1] }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { to: a, many: 2 }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { to: a, many: null }\n",
		ont + "policy:\n  a: { nope: true }\n",
		ont + "policy:\n  a: { orphan: null }\n",
		ont + "policy:\n  a: null\n",
		ont + "storage:\n  a: { path: as }\n",
		ont + "storage:\n  a: { layout: nope }\n",
		ont + "storage:\n  a: { layout: file, format: gjson }\n",
		ont + "storage:\n  a: { layout: file, format: null }\n",
		ont + "storage:\n  a: { layout: file, path: [] }\n",
		ont + "storage:\n  a: { layout: file, path: [1] }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: AD }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: { by: kind } }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: { by: kind, map: { x: X } } }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: { by: kind, map: {}, nope: 1 } }\n",
		ont + "storage:\n  a: { layout: file, template: true }\n",
		ont + "storage:\n  a: { layout: file, template: ../x }\n",
		ont + "storage:\n  a: { layout: file, id_date: later }\n",
	}
	// The walk reads a falsy layer block as empty; the schema still asks for
	// a mapping or null there, the one place it is stricter on purpose.
	stricter := []string{
		"ontology:\n  entities: []\n",
		"policy: false\n",
		"storage: ''\n",
	}
	for _, text := range stricter {
		doc := mustParseDoc(t, text)
		if msg := walkError(doc); msg != "" {
			t.Errorf("falsy-block probe is refused by the walk (%s):\n%s", msg, text)
		}
		if errs := jsValidate(s, s, doc, "$"); len(errs) == 0 {
			t.Errorf("falsy-block probe passes the JSON Schema:\n%s", text)
		}
	}
	for _, text := range accepted {
		doc := mustParseDoc(t, text)
		if msg := walkError(doc); msg != "" {
			t.Errorf("probe meant to be accepted is refused by the walk (%s):\n%s", msg, text)
			continue
		}
		if errs := jsValidate(s, s, doc, "$"); len(errs) > 0 {
			t.Errorf("the walk accepts this but the JSON Schema refuses it (%s):\n%s",
				strings.Join(errs, "; "), text)
		}
	}
	for _, text := range rejected {
		doc := mustParseDoc(t, text)
		if walkError(doc) == "" {
			t.Errorf("probe meant to be refused is accepted by the walk:\n%s", text)
			continue
		}
		errs := jsValidate(s, s, doc, "$")
		if len(errs) == 0 {
			t.Errorf("the walk refuses this but the JSON Schema accepts it:\n%s", text)
		}
		for _, e := range errs {
			if strings.Contains(e, "unknown to this test") {
				t.Errorf("probe refused by the validator, not the schema: %s", e)
			}
		}
	}
}

// walkError runs one document through what Resolve runs over the authored
// layers: mergeLayers, the vocabulary walk, then the storage matrix. "" means
// accepted.
func walkError(doc *omap.Map) string {
	layers, err := mergeLayers([]schemaDoc{{name: "probe.yaml", m: doc}})
	if err != nil {
		return err.Error()
	}
	raw := omap.New()
	ont := omap.New()
	ont.Set("entities", layers.entities)
	raw.Set("ontology", ont)
	raw.Set("policy", layers.policy)
	raw.Set("storage", layers.storage)
	sf, verrs := validateSchemaFile(raw)
	if len(verrs) > 0 {
		return smuggledError(verrs).Error()
	}
	if err := finishLayered(sf); err != nil {
		return err.Error()
	}
	return ""
}

func mustParseDoc(t *testing.T, text string) *omap.Map {
	t.Helper()
	m, err := ParseDoc("probe.yaml", text)
	if err != nil {
		t.Fatalf("parse probe: %v\n%s", err, text)
	}
	return m
}

func parseLayerFile(t *testing.T, path string) *omap.Map {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return mustParseDoc(t, string(raw))
}

func dig(t *testing.T, m *omap.Map, path []string) *omap.Map {
	t.Helper()
	var cur any = m
	for _, p := range path {
		switch x := cur.(type) {
		case *omap.Map:
			cur, _ = x.Get(p)
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil {
				t.Fatalf("bad index %q in %v", p, path)
			}
			cur = x[i]
		}
	}
	out, ok := cur.(*omap.Map)
	if !ok {
		t.Fatalf("no mapping at %v", path)
	}
	return out
}

// jsValidate is a draft-07 validator for the keywords LayerJSONSchema emits.
// It returns one line per failure; any keyword it does not know fails, so the
// schema cannot grow a keyword this test silently ignores.
func jsValidate(root, s *omap.Map, v any, at string) []string {
	var out []string
	fail := func(format string, args ...any) {
		out = append(out, at+": "+fmt.Sprintf(format, args...))
	}
	for _, kw := range s.Keys() {
		arg, _ := s.Get(kw)
		switch kw {
		case "$schema", "title", "description", "definitions":
		case "$ref":
			name := strings.TrimPrefix(arg.(string), "#/definitions/")
			defs, _ := root.Get("definitions")
			def, ok := defs.(*omap.Map).Get(name)
			if !ok {
				fail("unresolved $ref %v", arg)
				continue
			}
			out = append(out, jsValidate(root, def.(*omap.Map), v, at)...)
		case "allOf":
			for _, sub := range arg.([]any) {
				out = append(out, jsValidate(root, sub.(*omap.Map), v, at)...)
			}
		case "anyOf":
			var why []string
			for _, sub := range arg.([]any) {
				errs := jsValidate(root, sub.(*omap.Map), v, at)
				if len(errs) == 0 {
					why = nil
					break
				}
				why = append(why, errs...)
			}
			if why != nil {
				fail("matches no branch of anyOf")
				out = append(out, why...)
			}
		case "type":
			if !jsIsType(v, arg.(string)) {
				fail("%T is not %v", v, arg)
			}
		case "enum":
			if !slices.ContainsFunc(arg.([]any), func(e any) bool { return jsEqual(e, v) }) {
				fail("%v is not one of %v", v, arg)
			}
		case "const":
			if !jsEqual(arg, v) {
				fail("%v is not %v", v, arg)
			}
		case "pattern":
			if str, ok := v.(string); ok && !regexp.MustCompile(arg.(string)).MatchString(str) {
				fail("%q does not match %v", str, arg)
			}
		case "minItems":
			if list, ok := v.([]any); ok && int64(len(list)) < arg.(int64) {
				fail("fewer than %v items", arg)
			}
		case "items":
			if list, ok := v.([]any); ok {
				for i, it := range list {
					out = append(out, jsValidate(root, arg.(*omap.Map), it, fmt.Sprintf("%s[%d]", at, i))...)
				}
			}
		case "required":
			if m, ok := v.(*omap.Map); ok {
				for _, k := range arg.([]any) {
					if _, has := m.Get(k.(string)); !has {
						fail("missing %v", k)
					}
				}
			}
		case "properties":
		case "additionalProperties":
			m, ok := v.(*omap.Map)
			if !ok {
				continue
			}
			props, _ := s.Get("properties")
			for _, k := range m.Keys() {
				val, _ := m.Get(k)
				if props != nil {
					if ps, has := props.(*omap.Map).Get(k); has {
						out = append(out, jsValidate(root, ps.(*omap.Map), val, at+"."+k)...)
						continue
					}
				}
				switch ap := arg.(type) {
				case bool:
					if !ap {
						fail("unknown key %q", k)
					}
				case *omap.Map:
					out = append(out, jsValidate(root, ap, val, at+"."+k)...)
				}
			}
		default:
			fail("keyword %q unknown to this test's validator", kw)
		}
	}
	return out
}

func jsIsType(v any, typ string) bool {
	switch typ {
	case "null":
		return v == nil
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		switch v.(type) {
		case string, canon.Date, canon.DateTime:
			return true
		}
	case "object":
		_, ok := v.(*omap.Map)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	}
	return false
}

func jsEqual(a, b any) bool {
	fa, aNum := jsNumber(a)
	fb, bNum := jsNumber(b)
	if aNum || bNum {
		return aNum && bNum && fa == fb
	}
	switch a.(type) {
	case string, bool, nil:
		return a == b
	}
	return false
}

func jsNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, !math.IsNaN(x)
	}
	return 0, false
}
