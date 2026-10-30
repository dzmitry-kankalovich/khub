# Ingestion conflicts: the policy

**Status: design. Nothing on this page ships yet.** khub has no ingestion verb.
This note fixes the conflict policy before one is built, so the first importer
(facet's artifacts, an OKF bundle) is written against a contract instead of
inventing one.

Ingestion here means reading records from an outside source (a facet scan, an
OKF v0.2 bundle, a later connector) and writing them into a workspace as
entities. The source is re-read over time: the same records arrive again,
changed or not, after humans and agents have edited what the last run wrote.
The policy answers one question: what happens when the source and the
workspace disagree?

## The rule

**Ingestion never silently overwrites a non-draft value.** Every disagreement
it does not resolve under a declared strategy is reported as a conflict, and
every value it overwrites in a non-draft entity is reported too, whatever the
strategy. The local value stays until something decides otherwise.

The rule extends from field values to identity: **ingestion never merges two
entities on its own.** When an incoming record might be an entity that already
exists, the candidates are proposed with their evidence, and nothing is merged.

## What it inherits

Ingestion is one more writer through the core verbs, so every invariant in the
[design memo](design-memo.md) holds, and a few decide the shape of this design.

- **`draft` is the line.** khub's rules for untrusted content
  ([CLI](cli.md)) contain injected writes with provenance plus `draft`. An
  ingested entity is created `draft: true`, with `author` naming the source.
  While it stays a draft the source owns it, and a re-run may update it freely,
  including a value someone edited by hand. Publishing is what protects an edit.
  Setting `draft: false` is a human's or agent's publish decision. From then
  on the entity's values are no longer the source's to change. Ingestion never
  toggles `draft`, in either direction.
- **Create is exclusive.** A new entity is published exclusively, so a file
  that appeared since the scan is never replaced, and a taken slug is refused
  (`slug_taken`), never suffixed. Ingestion uses the same create path. An
  agent's "search before you write" habit is guidance, not a guarantee. okfm's
  probe, where `create` overwrote an existing id, is the failure that only an
  exclusive create rules out.
- **Capture is never blocked.** A record that lacks a required field is still
  written. `check` reports the gap. A conflict on one field does not stop the
  record's other fields, or other records, from being written.
- **khub never prompts.** A strategy is chosen in the schema or by a flag, never
  asked for in the middle of a run.
- **Determinism.** Records are processed in the source's order (a bundle's paths
  sorted bytewise), and fields in schema declaration order. The same workspace
  and the same source produce the same report, byte for byte.
- **Zero per-type code.** Matching, comparing and reporting read the resolved
  schema. A per-field strategy is schema configuration, not a branch in a
  surface.
- **Nothing is persisted beside the truth.** The report is derived. Re-running
  ingestion against an unchanged source reproduces it, so it is never stored in
  the workspace.

## Field values

Once a record is matched to a local entity (see Identity below), each field
the schema declares is compared, `body` included. Undeclared keys follow the
same table. They are extensions, which khub preserves verbatim.

| Local entity | Ours | Theirs | Outcome |
|---|---|---|---|
| any | absent | present | **filled**: theirs is written |
| any | present | absent | **kept**: ingestion never deletes a field |
| any | equal | equal | **unchanged** |
| draft | present | differs | **updated**: theirs is written |
| non-draft | present | differs | **conflict**: resolved by the field's strategy, `manual` by default |

Equality compares values as khub would write them, so `2026-01-05` and a date
that renders to it are equal, and a list compares in order.

Relations follow the same table, with one difference for a many-valued
relation. A target the source adds is **filled**, the same as `khub link`. A
target the source no longer lists is **kept**. A single-valued relation that
differs is a conflict like any field. An incoming target that does not resolve
is refused for that field, as on every write (referential integrity), and is
reported under `refused`, not as a conflict.

## Strategies

A strategy decides a conflict. The menu is closed, and each name means one thing:

| Strategy | Decides | Reported as |
|---|---|---|
| `manual` (default) | ours stays | `conflict`, unresolved |
| `prefer-source` | theirs is written | `overwritten` |
| `prefer-newest` | the side with the later date is written. Ours is dated by the entity's `updated`, theirs by the record's own modification date. When either is missing or the dates tie, falls back to `manual` | `overwritten` or `kept`, or `conflict` on a fallback |
| `prefer-ours` | ours stays | `kept` |

`prefer-ours` is how a deliberate local divergence stops reappearing in every
run's report. `prefer-source` and `prefer-newest` still list each overwrite, so
nothing changes in a published entity without a line saying so.

**Where the strategy is set.** The first version takes `manual` only. Per-field
configuration comes later. It belongs in `policy.yaml`, because it is a
workspace gate and not part of the domain:

```yaml
# .khub/policy.yaml  (later; illustrative)
ingest:
  conflicts:
    default: manual
    fields:
      requirement.statement: prefer-source
      capability.title: prefer-ours
```

A key naming a type or a field the schema does not declare is a schema error
when the schema resolves, like any other dangling schema reference.

**Resolving a manual conflict** uses the existing verbs. `khub edit <id> <field>
<theirs>` accepts the source's value, which makes the two sides equal, so the
next run reports nothing. Keeping ours for good means setting that field to
`prefer-ours`. Until then the conflict is reported on every run, by design.

## Identity

Before values can be compared, an incoming record has to be matched to an
entity, or to none. Matching is a ladder, and only the first rung merges.

1. **Source id.** The record carries the source's own id, and the workspace
   holds an entity of the mapped type whose `aliases` contain it, written
   `<system>:<id>` (as in `facet:FUNC-007`). This is the `source_id` alias the
   design memo describes. It is the same entity, and the record goes to field
   comparison.
2. **Slug collision.** No alias matches, but the slug the record would mint (or
   the id it brings) is already taken in that type. This is not a match. The
   record is not written, and the report carries an **identity proposal** whose
   evidence is the collision.
3. **Likeness.** No alias matches and the slug is free, but the workspace holds
   entities that resemble the record (an equal title, a matching alias of
   another system, a strong search hit). The record is created as a new draft,
   and the report proposes the candidates with their evidence, so a duplicate
   is visible from the run that made it.

A proposal never merges anything. Accepting one is an edit: add the record's
`<system>:<id>` to the chosen entity's `aliases`, and the next run matches it on
rung 1. Rejecting a rung-2 proposal means creating the new entity under another
slug with `khub add --id`, carrying the alias. A rung-3 proposal is rejected by
leaving it alone, or by removing the duplicate draft after merging it by hand.
The evidence each rung gathers, and how strong a likeness has to be, belong to
the alignment-ladder work, which is the mechanism this rule constrains.

An alias that two entities both claim is already a `check` failure
(`alias_conflicts`), so a wrong acceptance is caught by the gate that exists.

## The report

Ingestion prints one document on stdout, in the CLI's JSON dialect, with the
same record shape as every other command (`id`, `type`, `slug`):

```json
{
  "source": {"system": "facet", "path": "scan/functional.json"},
  "created": [{"id": "capability/billing", "source_id": "facet:FUNC-007", "draft": true}],
  "updated": [],
  "filled": [],
  "unchanged": 14,
  "conflicts": [
    {
      "id": "requirement/refund-window", "type": "requirement", "slug": "refund-window",
      "field": "statement",
      "ours": "Refunds are accepted within 30 days.",
      "theirs": "Refunds are accepted within 14 days.",
      "source": {"system": "facet", "id": "BR-012", "path": "scan/rules.json"},
      "strategy": "manual",
      "resolution": "unresolved"
    }
  ],
  "identity": [
    {
      "source": {"system": "facet", "id": "UC-031", "path": "scan/use-cases.json"},
      "rung": "slug_collision",
      "written": false,
      "candidates": [{"id": "use-case/checkout", "evidence": ["slug_taken"]}]
    }
  ],
  "refused": []
}
```

`ours` and `theirs` are the values as JSON, so a list or a date compares at a
glance. Overwrites under `prefer-source` and `prefer-newest` appear in
`conflicts` with their `resolution`, so the one array lists every disagreement.

`--dry-run` prints the same document and writes nothing, and takes no lock.

**Exit codes.** 0 when nothing is left to decide. 1 when any conflict is
unresolved or any identity proposal stands. Everything else the run could write
has been written, and the report names what needs a decision, which is what 1
already means for `validate` and `check`: the workspace is what needs attention,
not the call. 2 when the call is wrong (an unreadable source, a record of a type
the schema does not map) and nothing was written.

## Writes

One run holds the workspace lock for its whole scan, comparison and every
publication, so no other writer interleaves. Each file publishes atomically,
but a run is not one transaction across files. An interrupted run leaves a
prefix written. A re-run is the recovery: every written record now compares
equal or matches by alias, and the report picks up from where it stopped.

## Later

- **Per-field strategies** in `policy.yaml`, as sketched above, and a run-wide
  `--prefer <strategy>` flag. The first version is `manual` only.
- **A three-way compare.** With the value the source last wrote as a base, a
  field the workspace never touched could take the source's change without a
  conflict. The base would come from git (the field's value in the last commit
  that ingestion made), never from a stored file. This needs a convention for
  identifying ingestion commits, so it waits until the two-way rule is in use.
- **The alignment ladder's evidence**: what counts as a likeness on rung 3, and
  how proposals are ranked.
- **Collection rows** follow the same rules. A row's identity is its slug, and
  its locator joins the report records the way it joins `add` and `get`.
- **Fixtures.** okfm's example bundles are OKF v0.2 consume-path inputs. The
  first parity cases are an exclusive-create sentinel (an ingested record never
  replaces an entity that exists) and a published-entity sentinel (a re-run
  against a changed source leaves a `draft: false` value unchanged).
