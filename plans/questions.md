# Open questions

Shared scratchpad for things that need the maintainer's input. Tasks with open
questions are skipped (per instruction), everything else proceeds.
Answer inline and delete entries as they resolve.

## Loader append without an update key

Load and API assets currently map keyless `append` to Sling snapshot mode.
Sling adds `_sling_loaded_at` by default, which makes repeated snapshots useful
but also changes the declared output schema. Which contract should Renart own?

- Keep the bookkeeping column and explain/surface it in the materialization and
  column UI.
- Disable the column (`SLING_LOADED_AT_COLUMN=false`), matching Python's Sling
  upload leg, and keep append schema-preserving.
- Reject keyless append and require an update key.

Until this is decided, the existing snapshot behavior stays unchanged.

Previously resolved threads — landing-page direction, onboarding concept, the
ingestr flag's home, and the alpha docs scope — are in git history; their
outcomes shipped and live in `../architecture/`.
