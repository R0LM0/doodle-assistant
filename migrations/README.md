# migrations

Versioned, forward-only SQL migrations applied in filename order, one transaction
per file.

Naming: `NNNN_short_description.sql` (e.g. `0001_init.sql`, `0002_hnsw_index.sql`).

Rules:

- Every migration is idempotent-safe to re-run inside its own transaction
  (`CREATE TABLE IF NOT EXISTS`, `CREATE EXTENSION IF NOT EXISTS`).
- The embedding column dimension must match the model reported by
  `scripts/healthcheck.sh` (currently `embeddinggemma` -> `vector(768)`,
  `mxbai-embed-large` -> `vector(1024)`). Changing models means a new column or a
  full re-embed, never an in-place `ALTER`.
- Never edit a migration that already ran; add a new one.
