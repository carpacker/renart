# Polyglot typechecking reach

Status: evaluation after the July 2026 typecheck/LSP parity work.

## Decision summary

Keep the incremental shared-validator architecture for now. Polyglot 0.6.2 has
several useful capabilities beyond the calls Renart originally used, but the
best adoption order is:

1. Enable expression type checks in the existing schema-validation call.
2. Use structured parse offsets and the standalone data-type parser.
3. Introduce one cached compact-analysis result, but use it selectively rather
   than making it the primary output-inference call.
4. Enrich Renart's schema payload with constraints before enabling reference
   checks.
5. Keep style and strict-syntax rules behind an explicit lint policy rather
   than making them core typecheck failures.

Steps 1 through 4 are implemented. They add no new runtime and preserve the
shared pipeline typecheck/LSP diagnostic path. The remaining items are
deliberately staged below.

## Current use

Renart calls the embedded SDK WASM directly through the pooled wazero runtime:

| Polyglot capability | Renart use |
| --- | --- |
| `parse` | AST-backed SQL context, dependency extraction, syntax diagnostics |
| `tokenize` | exact identifier/token spans for editor features |
| `validate_with_schema` | strict identifiers, expression types, declared constraints, and ambiguous join columns |
| `annotate_types` | output-column inference and declared-output drift |
| `analyze_query` | cached scope-aware expansion when annotated output names are incomplete |
| `parse_data_type` | cached dialect-aware comparison of declared/inferred types |
| `format_sql_with_options` | SQL formatting |

The parser's structured `errorStart`/`errorEnd` fields are now the primary
syntax diagnostic range. Textual line/column extraction remains a fallback for
older payloads.

## High-value unused capabilities

### 1. Compact query analysis (`analyzeQuery`) — selectively adopted

One response contains projection
names, detailed `typeHint` values (including `DECIMAL(10,2)`), conservative
nullability, transform kinds, star expansions, CTE/set-operation facts,
visible relations, transitive base tables, and per-projection upstream column
references.

It could replace pieces of three current implementations:

- manual output extraction from the annotated AST;
- parts of the tolerant relation/projection walkers;
- separate lineage/source-method inference.

It is not added to every LSP request. Direct wazero benchmarks on the July 2026
development host measured roughly 1.0–1.2 ms versus 0.50–0.53 ms for a short
query, 3.9–4.1 ms versus 1.1–1.3 ms for a CTE-heavy query, and 27 ms versus
2.3–2.5 ms for a 50-column projection (`analyze_query` versus
`annotate_types`). Making compact analysis the universal primary path would
therefore regress large-project graph latency.

The implemented placement keeps annotated-AST inference as the fast path and
calls compact analysis only when explicit output names are incomplete, notably
for star expansion through joins and CTEs. Results are held in a 256-entry LRU
keyed by `(SQL, normalized dialect, deterministic schema payload)`, so fixpoint
rounds and identical revision builds reuse them. Canceled or failed calls never
populate the cache. Projection facts still have no source spans, so
tokenization remains necessary for precise editor ranges.

The cached result is now also used by declared-output validation when the fast
annotated AST cannot infer a name/type through nested CTEs or stars, and when an
asset explicitly declares a non-null output contract. Polyglot's nullability
facts account for expressions and outer-join sides; Renart warns only when a
provably nullable projection conflicts with `nullable: false`. Unknown facts
and the safe inverse stay silent. Star name sets are accepted only when every
physical source has complete schema confidence.

The remaining opportunity is to expose cached upstream/base-table facts as
column provenance or dependency explanations. Per-edit adoption should still
replace an existing WASM call, not add another call to the hot path.

### 2. Rich schema constraints and reference checks — adopted selectively

Polyglot accepts column nullability, primary/unique keys, foreign keys, and
column references. The canonical graph now preserves Bruin's authoritative
`nullable`, `primary_key`, and `foreign_key` column metadata across the parsed
pipeline, web workspace DTO, and filesystem stdio-LSP loader. The shared
validator serializes those fields in deterministic Polyglot schema payloads.
Inferred layers contribute no constraints, and foreign keys whose target is not
represented in the current snapshot are withheld so one partial external
relation cannot emit a schema-level error in every document.

`checkReferences` is enabled in strict mode. Its high-confidence ambiguous
unqualified join-column diagnostic is delivered by pipeline typecheck, HTTP
LSP, and stdio LSP with the same token range. Cross-dialect fixtures cover all
Renart SQL dialect mappings.

Polyglot's current `W220`/`W221`/`W222` join-quality warnings remain outside
core typechecking. Evaluation found that `W220` warns for an explicit
`CROSS JOIN` while missing comma joins and `JOIN ... ON TRUE`, and `W221` also
warns for legitimate joins on an alternate key. Those heuristics belong in a
future opt-in lint profile. Unique keys are likewise not guessed because Bruin
does not currently expose an authoritative unique-key column contract.

### 3. Schema-aware lineage

`lineageWithSchema` and `getSourceTables` can improve column provenance and
dependency explanations, but they are not a better core primitive for current
typechecking. Evaluation against nested CTEs, derived tables, joins, and stars
showed that `analyzeQuery.projections[].upstream` already resolves transitive,
fully qualified base columns. `getSourceTables` is one call per output column
and returned unqualified table names in qualified-table fixtures. Schema-aware
lineage also runs once per column and returns a much larger AST-backed tree.

Whole-query dependency extraction remains on the existing `parse` walk:
`analyzeQuery.baseTables` handles SELECTs well but rejects DML and
multi-statement scripts, while Renart's compatibility contract covers both.
The dedicated lineage APIs should therefore be introduced only with a concrete
column-provenance UI that needs intermediate CTE/derived nodes; compact analysis
is the canonical fact set for typechecking.

### 4. Optional validation modes

Polyglot exposes semantic warnings (`SELECT *`, aggregation/style rules),
strict-syntax checks, and dialect-strict validation. These are lint policy, not
schema typechecking. Some supported engines intentionally accept permissive or
dialect-specific forms, so enabling them globally would turn preferences into
blocking errors. A future project-level lint profile could opt into them while
reusing the same diagnostic adapters.

## Lower-value for typechecking

`transpile`, `generate`, SQL diff, query plans, and OpenLineage output are useful
for dialect migration, refactoring, explain views, and metadata export, but do
not directly improve static type correctness. `getDialects` could back a
compatibility test for Renart's asset-type mapping; it should not replace that
mapping because asset execution semantics remain Renart/Bruin-owned.

## Validation required for a future provenance phase

- Include nested/recursive CTEs, derived tables, set operations, templates, and
  duplicate projection names in lineage fixtures.
- Define a canonical column-provenance graph contract before storing the large
  dedicated lineage response in the LSP graph or workspace payload.
- Preserve the parity invariant: any new typecheck diagnostic must have an
  honest document or asset/header delivery in both HTTP and stdio LSP.
