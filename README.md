# doodle-assistant

RAG + agentic assistant for a cartoon-drawing app, built in Go as a learning
project: hexagonal architecture, `net/http` straight to Ollama, Postgres +
pgvector, no SDKs.

## Status

| Phase | Scope | State |
| --- | --- | --- |
| 0 | Setup: module, pgvector compose, preflight | done |
| 1 | Ingestion: markdown chunking, embeddings, idempotent upsert | done |
| 2 | Classic RAG: `Ask` with top-K + streaming answer | next |
| 3 | Evaluation: recall@K dataset | pending |
| 4 | Tool calling on an in-memory canvas | pending |
| 5 | Agentic RAG: `search_docs` as a tool + evidence judge | pending |
| 6 | HTTP/SSE API + Vite/TS UI | pending |
| 7 | Optional: Docker image + Neon | pending |

## Prerequisites

- Go 1.27.3 (`go version`)
- Docker Desktop with compose
- Ollama running on the host (`OLLAMA_BASE_URL`, default `http://localhost:11434`)
- Models: `ollama pull gemma-4-e4b` (chat) and `ollama pull embeddinggemma` (embeddings)

Embedding model choice: `embeddinggemma` (768 dims, 2048 ctx, multilingual) is the
project default because the knowledge base is Spanish; `mxbai-embed-large` (1024
dims, 512 ctx, English-centric) stays installed as the comparison candidate for
the phase 3 chunk-size evaluation.

## Quickstart

```bash
# 1. Local Postgres + pgvector (host port 5434: 5432/5433 are already in use)
docker compose up -d

# 2. Preflight: container health, pgvector, Ollama, models, embedding dim
scripts/healthcheck.sh

# 3. Apply migrations, index the knowledge base, inspect the result
go run ./cmd/assistant migrate
go run ./cmd/assistant ingest
go run ./cmd/assistant stats
```

## Ingestion (phase 1)

`ingest` walks `DOCS_DIR` (default `docs`), cuts every markdown file at heading
boundaries, embeds the chunks in batches and stores them in one transaction per
document.

Idempotency comes from `documents.content_hash`: when a file's normalized content
is unchanged it costs one indexed `SELECT`, no chunking and no embedding calls.
Re-running `ingest` on an unchanged tree reports
`scanned=8 skipped=8 indexed=0 chunks=0 embed_calls=0`.

Tunables: `EMBED_BATCH_SIZE` (default 32) and `EMBED_DIM`, which must match
`vector(N)` in the migration. Chunk defaults live in `domain.DefaultChunkOptions`
(1200 chars max, 150 overlap, 200 min) and are tuned in phase 3 against the
evaluation dataset, not by intuition.

The knowledge base is written in Spanish on purpose: it exercises the multilingual
embedding model the project chose.

## Tests

```bash
go test ./...                     # unit tests: no network, no database
go test -race ./...               # same, with the race detector
go vet ./...

# integration: needs the compose container up
DATABASE_URL='postgres://doodle:doodle@localhost:5434/doodle?sslmode=disable' \
  go test -tags integration ./internal/adapters/postgres/
```

`internal/archtest` fails the build if `domain`, `app` or `ports` ever import an
adapter.

## Environment variables

Configuration is environment-only. `docker-compose.yml` ships the same defaults,
so nothing is required to boot. To override, create a local `.env` (gitignored)
with any subset of:

```
POSTGRES_USER=doodle
POSTGRES_PASSWORD=doodle
POSTGRES_DB=doodle
POSTGRES_PORT=5434
DATABASE_URL=postgres://doodle:doodle@localhost:5434/doodle?sslmode=disable

OLLAMA_BASE_URL=http://localhost:11434

CHAT_MODEL=gemma-4-e4b          # must report the "tools" capability before phase 4
EMBED_MODEL=embeddinggemma      # 768 dimensions, 2048 ctx, multilingual
EMBED_DIM=768                   # must match vector(N) in migrations
```

Moving to Neon later is a `DATABASE_URL` change only; pgvector stays available
there, so no code path depends on the local container.

## Layout

```
cmd/assistant/          composition root (the only place that knows adapters)
internal/domain/        Document, Chunk, chunker, content hash - stdlib only
internal/app/           use cases: IngestDocs (Ask, RunAgent next)
internal/ports/         Embedder, VectorStore, DocumentLoader
internal/adapters/      ollama, postgres, filesystem, memory, cli (http in phase 6)
docs/                   knowledge base markdown (Spanish)
migrations/             versioned SQL, embedded in the binary
 eval/                   retrieval evaluation dataset
scripts/                preflight / helpers
```

`internal/domain` and `internal/app` must never import `internal/adapters`; an
architecture test enforces this from phase 1 on.
