# Ingestion alignment: resolving what a source refers to

**Status: design. Nothing on this page ships yet.** khub has no ingestion verb.
This note is the companion of [ingestion-conflicts.md](ingestion-conflicts.md),
which sets the policy an importer follows: never silently overwrite a
published value, never merge two entities on its own, and match an incoming
record to an entity down a three-rung ladder. That note leaves two things to
this one, and this note adds nothing that contradicts it:

- **References.** A record names other things: the capability a use case
  belongs to, the component a requirement is realized in. Each such reference
  has to land on an entity before the edge can be written, and an unaligned
  reference is how an importer mints duplicates.
- **Evidence.** What counts as a likeness on rung 3, how candidates are ranked,
  and how the ladder is measured.

KAG's knowledge builder makes the same split: extraction produces mentions,
and a separate alignment pass links them to existing nodes before anything is
written. khub's version of that pass is deterministic, reads only the
workspace and the source, and never decides identity by resemblance.

## Terms

- **Record**: one item the source maps to an entity. Its identity is matched
  by the ladder in ingestion-conflicts.md, unchanged here.
- **Reference**: a value in a record that the source maps to a relation. It
  takes one of three forms, and the importer's source mapping says which form
  each field carries:
  - a **source id**, the id of another record of the same system, written as
    `<system>:<id>` (`facet:FUNC-007`), the same `source_id` alias a record
    carries;
  - a **khub id**, a slug or `type/slug`, when the source speaks khub ids (an
    OKF bundle exported from a khub workspace, a hand-written import file);
  - a **mention**, a name with no id behind it (`"the billing service"`).
- **Target types**: the types the relation allows. A typed relation has one or
  a few; an `any` relation has all of them.

A slug is lowercase `a-z`, `0-9` and hyphens, so a `<system>:<id>` can never
equal one. A source id resolves only through `aliases`, and is never taken
for a slug.

## The reference ladder

Each reference is resolved in pass two of a run, after every record has been
written or refused (ingestion-conflicts.md, Field values). So a reference to a
record of the same run already resolves on rung 1.

| Rung | Applies to | Lookup | On a hit | On a miss |
|---|---|---|---|---|
| 1. Source id | source id | the alias across every type, case-insensitively, as lookup resolves aliases | in a target type: the edge is written to it. In another type: refused, with a `type_mismatch` proposal | rung 3 |
| 2. khub id | khub id | the ID lookup every khub verb uses: exact slug, slug case-insensitively, then aliases | in a target type: the edge is written, stored as the real slug, as `link` stores it | refused as an unresolved target, as `link` refuses it |
| 3. Stub | an unresolved source id | none: a draft is minted in the target type, carrying the alias | the edge is written to the new draft | see below |
| 4. Likeness | a stub, a mention | candidates scored from the workspace (Evidence, below) | proposed, never linked | nothing more |

Rungs 1 and 2 are the only rungs that write an edge to an entity that already
exists, and both are exact. The issue that asked for this note listed the
lookups as `(type, slug)`, then alias, then `source_id`. In khub the source id
is an alias with a namespace, and the ID lookup already tries the slug before
the aliases, so the three exact lookups are rungs 1 and 2 by form.

**Rung 3, the stub.** A source id that resolves nowhere names a record the
source has not sent yet, or one this run did not include. The importer mints a
draft for it, so the edge can be written and capture is not blocked:

- its type is the relation's target type. A relation with more than one target
  type gives no type to mint, so the reference is refused as `unresolved` and
  nothing is guessed;
- `draft: true`, `author` naming the source, and `aliases: [<system>:<id>]`, as
  ingestion-conflicts.md writes a created record;
- its `title` is the mention text the source puts beside the id, when it puts
  one, else the bare id. The slug is minted from it by the type's own scheme,
  as `add` mints it. When that slug is taken, or is another entity's alias,
  the stub is not written, the report carries a `slug_collision` proposal as
  rung 2 of the record ladder does, and the reference is refused.

When the record itself arrives in a later run, it matches the stub on rung 1
of the record ladder and fills its fields. The stub is still a draft, so the
source owns it and nothing is a conflict.

**Provenance** is what the stub carries and nothing more: `author` names the
source, the alias names the record, and the commit that wrote it is in git. No
base field is added for extraction provenance, because the path and the run
are in the report and the history, and a field on the entity would go stale
the first time a person edits it.

**Mentions are not minted.** A mention has no id the source will repeat, so a
stub minted for it has no key a re-run can match: the next run would mint the
same slug and collide with its own stub, or mint another. A mention is
reported as `unresolved` with its likeness candidates, and no edge is written.
The fix is in the extractor, which should emit ids, or in the workspace, where
`khub link` writes the edge a person chose.

## Evidence

Likeness runs for a record created on rung 3 of the record ladder, for a stub,
and for a mention. It scores entities of the target types (the mapped type,
for a record) from the workspace as the run found it: an entity this run
created is never a candidate, and the subject is never its own. Within-run
look-alikes surface on the next run, when they are drafts in the workspace and
ingestion-conflicts.md recomputes a draft's proposals.

Each candidate carries the evidence that put it there, as stable codes:

| Code | Meaning |
|---|---|
| `title_equal` | the candidate's `title` equals the subject's title or mention, case folded and with whitespace runs collapsed |
| `alias_equal` | one of the candidate's aliases equals the subject's title or mention, or carries the same id under another system (`okf:FUNC-007` beside `facet:FUNC-007`), compared case-insensitively |
| `title_match` | a `--plain` search of the target types, with the subject's title or mention as the query, gives the candidate a `title_match` of at least 0.8 |
| `text_match` | the same search, with the title and `description` as the query, gives a `match` of at least 0.8 |

The search is the one `khub search --plain` runs today, with its scoring: each
distinct word weighted by inverse document frequency, cut at 64 words, built
in memory over the workspace. The CLI takes one `--type`; the importer calls
the same core search over every target type at once. Likeness needs no index
and no new scoring.

**Ranking.** Candidates sort by their strongest code in the order of the table,
then by the number of codes they carry, then by search `score`, then by `id`
bytewise. The report lists at most five. The same workspace and the same
source give the same list.

**Fixed in the first version.** The 0.8 thresholds and the cap of five are
constants, not schema configuration. The goldens below are how they get
tuned; a `policy.yaml` knob waits until a tuned value differs between
workspaces.

Evidence never resolves anything. Two entities with equal titles are a
proposal, however strong, and the only ways a proposal becomes an edge or a
merge are the verbs ingestion-conflicts.md lists under Identity, plus
`khub link` for a mention.

## Two modes

KAG 0.7 reports that a lightweight build cut its token cost by 89% with
little loss. khub spends no tokens aligning, but the cost has a khub-side
analogue: every proposal is something an agent or a person reviews. So the
importer ships both modes from the start:

- **`--align exact`** runs rungs 1 to 3 only. Unresolved source ids still get
  stubs, and a mention is still reported as `unresolved`, but no candidates
  are gathered. A duplicate can be minted as a draft. It stays visible to
  `check`, and a later `full` run proposes it.
- **`--align full`**, the default, adds rung 4 for every subject it applies to.

Both modes write the same edges. They differ only in what the report proposes,
so switching between runs never moves an edge.

## The report

The `identity` array of ingestion-conflicts.md's report carries reference
proposals too. A row about a reference adds `from` (the entity the record
became, with `id`, `type` and `slug`) and `relation` (the predicate), and its
`rung` is `source_id`, `slug_collision`, `likeness` or `unresolved`:

```json
{
  "source": {"system": "facet", "id": "UC-031", "path": "scan/use-cases.json"},
  "from": {"id": "use-case/uc-refund", "type": "use-case", "slug": "uc-refund"},
  "relation": "capability",
  "reference": "the billing capability",
  "rung": "unresolved",
  "written": false,
  "candidates": [
    {"id": "capability/cap-billing", "type": "capability", "slug": "cap-billing", "evidence": ["title_match"]}
  ]
}
```

A stub is a `created` row with `"stub": true`. A refused reference is a
`refused` row, as any value that fails validation is. A non-empty `identity`
exits 1, as ingestion-conflicts.md sets out, so a mention with no candidates
still fails the run: something was not written.

## Measuring it

The planned ingestion goldens measure type and edge precision and recall of an
extraction. Alignment joins them with three numbers per golden. A golden is a
source, a starting workspace, and for every record and reference the entity it
should land on, or "new".

- **Link precision**: of the edges rungs 1 and 2 wrote, the share that land
  where the golden says. It must be 1.0, since both rungs are exact. Anything
  lower is a bug, and this number is a gate.
- **Proposal recall at five**: of the subjects the golden says are an existing
  entity but the exact rungs missed, the share whose right entity is among the
  candidates.
- **Proposal precision**: of all candidates proposed, the share that are right.

Recall and precision of proposals are reported, not gated, and trade against
each other through the thresholds. They are measured in both modes, which is
the number that says what `exact` gives up. okfm's example bundles are the
first OKF v0.2 inputs. Nothing in a golden is an LLM call: the extraction is an
input, and the goldens score khub's half.

## Across a workspace boundary

A reference that names an entity in another workspace is never matched by
likeness and never stubbed. Federation, when it exists, resolves only exact
ids across the boundary, and anything else is reported as `unresolved`. A
guess at a boundary is a wrong edge in someone else's graph.

## Later

- The ingestion verb itself, with ingestion-conflicts.md.
- Tuned thresholds, and a `policy.yaml` key for them if workspaces need
  different ones.
- Likeness for a mention against entities this run created, which needs a
  second scoring pass after pass one.
- The goldens harness, and its first fixtures from okfm's bundles.
