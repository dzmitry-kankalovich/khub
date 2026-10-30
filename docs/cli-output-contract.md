# khub CLI output contract

What a program reading khub's output may rely on: an agent, a CI job, the
Claude Code plugin, or an ingester that federates workspaces through
`query`, `get` and `schema --format json`. This page is the rules every
command follows. The per-command record shapes are in [`cli.md`](./cli.md), and
the flag surface is in [`cli-reference.md`](./cli-reference.md).

The parity fixtures under `parity/cases/` pin the bytes of every rule below. A
change that moves them is a behaviour change, and it gets a release note.

## When stdout is JSON

khub prints JSON when `--format json` is given **or** stdout is not a terminal.
A program never needs the flag, though passing it does no harm. This holds for
reads and writes alike, and for refusals (see [Refusals](#refusals)).

Exceptions, each of which the caller opts into or which is a human command:

| Output | When |
|---|---|
| Bare ids, one per line | `query --format ids`, `search --format ids` |
| The entity's file bytes, or its row | `get <id> --format raw` |
| A text tree | `impact --format tree` |
| Prose | the operator commands `reindex`, `viz`, `serve`, `backfill` and `wire`, which take no `--format` |
| The version number alone | `--version` |
| Help text | `--help` |

`--format table` (or `text`) on a pipe is still JSON. The gate is the stream,
and an explicit table only applies on a terminal.

`TTY_COMPATIBLE` (`0` or `1`) and then `FORCE_COLOR` override the terminal
check, in that order. Nothing else does.

## One JSON value per invocation

A JSON-mode invocation prints exactly one JSON value on stdout, on one line,
followed by a newline. A failure prints nothing else on stdout before it, so a
consumer never has to choose between two documents. A shell chain of khub calls
prints one line per call, which is how the plugin reads a chain.

The value is either the command's payload (an object, or an array for list
commands such as `query`, `search`, `neighbors` and a batch `get`), or the
[refusal envelope](#refusals). An empty result is `[]` with exit 0, not a
refusal.

The JSON dialect:

- Separators are `", "` and `": "`.
- Every non-ASCII character is escaped as `\uXXXX`, with surrogate pairs above
  the Basic Multilingual Plane, so the stream is pure ASCII whatever the
  workspace holds.
- Dates and timestamps are ISO 8601 strings.
- **Key order is part of the contract.** Fields come in the order `cli.md`
  documents, frontmatter keys in file order, and schema keys in declaration
  order. A consumer may still treat objects as unordered.
- The refusal envelope's optional keys are absent when they do not apply,
  never `null`.

Entity records carry the qualified `id` (`type/slug`) and separate `type` and
`slug` keys. Key on `id`: a bare slug may name entities of two types.

## stderr

stderr is for people. A program reads stdout and the exit code, and treats
stderr as a log.

- A refusal in human mode prints its message on stderr. In JSON mode it
  prints nothing there.
- A usage error (an unknown command or flag, a missing argument) is plain text
  on stderr in every mode, with exit 2. It happens before a command runs, so
  there is no JSON envelope. Its "Did you mean" hint uses the same distance
  measure as the envelope's `suggestion`.
- An advisory line that the caller must see in every format starts with
  `note: `. `search` uses one when `--limit` dropped rows, when nothing
  matched, or when files could not be parsed. It is printed before the output,
  so it survives `| head`.

## Exit codes

| Code | Meaning | What the caller does next |
|---|---|---|
| `0` | Success. For `validate` and `check`, the gate passed, perhaps with gaps that never fail it. | Continue. |
| `1` | A gate ran and failed (`validate`, `check`). The workspace is wrong, not the call. stdout holds the full report. | Fix what the report names, then run the gate again. |
| `2` | Refused: a usage error, a call the schema forbids, a workspace or schema that cannot be read, or an OS error. Nothing was written. | Correct the call or the configuration and retry. In JSON mode, `error.code` says which. |

A broken schema is already exit 2, before any entity is examined. So a CI job
can tell "the workspace has findings" (1) from "the gate could not run" (2).
khub does not split 2 any further, into a configuration error and a usage
error, as some tools do: the corrective action is the same (fix the input and
run again), and `error.code` carries the difference for anyone who needs it.
A new exit code would need a different action to go with it.

Two codes are refusals that are not clean "nothing was written" cases.
`write_durability_uncertain` means the write or removal happened but could not
be confirmed durable, so read the file before retrying. `upgrade_recovery_failed`
means an upgrade's rollback needs a human to inspect the journal the message
names.

## Refusals

In JSON mode, a refusal is one object with a single `error` key:

```json
{"error": {"code": "enum_violation", "message": "'functionl' is not a valid kind (functional, non-functional, constraint, business-rule)", "field": "kind", "expected": ["functional", "non-functional", "constraint", "business-rule"], "suggestion": "functional"}}
```

| Key | Presence | Meaning |
|---|---|---|
| `code` | always | A stable snake_case identifier. Branch on this. |
| `message` | always | The same sentence a terminal user sees. It is pinned prose and names the corrective flag or command where there is one, but it is written for a reader. Do not parse it. |
| `field` | optional | The schema attribute or relation the refusal is about, spelled as the call spelled it. For `filter_error`, `illegal_predicate` and `strict_unknown_field` it is the undeclared name the call used. |
| `expected` | optional | The closed set of values the call could have used, in declaration order: an enum's members, the declared types, fields or relations, the known presets, or the qualified ids an ambiguous name could mean. |
| `suggestion` | optional | The one value nearest what the call gave, to put in its place. It is a value, not a command. |

The optional keys come in this order and are absent when they do not apply. A
refusal with nothing to add is the bare `{code, message}` pair.

`suggestion` is held to a tight budget, because a wrong guess costs an agent
more than none. It allows one edit per three characters typed, never more than
two, and the comparison ignores case. Fewer than three characters match on case
alone. A call that is far from every candidate gets no suggestion. An id
suggestion keeps the shape the call used, a bare slug or a qualified
`type/slug`, and is qualified anyway when the bare slug would be ambiguous. So
the suggestion always resolves to exactly the entity it names.

Which codes carry which keys:

| Code | `field` | `expected` | `suggestion` |
|---|---|---|---|
| `enum_violation` | the attribute | its members | nearest member |
| `pattern_violation`, `number_violation`, `invalid_path_value` | the attribute | | |
| `strict_unknown_field` | the undeclared key | | |
| `id_prefix_undecided` | the deciding attribute | its members | |
| `filter_error` | the undeclared name | the declared names | nearest name |
| `illegal_predicate` | the undeclared predicate | the type's relations | nearest relation |
| `cardinality_violation` | the relation | | |
| `referential_integrity` | the relation | | nearest id the relation can target |
| `lookup_error` | | | nearest id in the workspace |
| `ambiguity_error` | | the qualified ids it could mean | |
| `unknown_type` | | the declared types | nearest type |
| `unknown_preset` | | the shipped presets | nearest preset |
| `bad_target` | | `claude`, `agents`, `both` | nearest target |

Every other code carries `code` and `message` only. A later release may add a
key to a code that has none today. A program should read the keys it knows and
ignore the rest.

`error.code` values are stable. A new code, or a refusal moving from one code
to another, is a release note. Gate findings are not refusals: `validate` and
`check` report them as data in their payloads (exit 1), each with its own stable
location. See [Reading check output](./cli.md#reading-check-output).

## Stability

- Within this contract, a release may add an optional key, a new code, or a
  new command. It does not rename or remove a key, change a key's type, or
  reuse a code for a different meaning without a release note in
  `CHANGELOG.md`.
- The plugin records the oldest khub whose output it reads (`MIN_KHUB` in
  `plugin/hooks/workspace.ts`). Use `khub --version` the same way.
