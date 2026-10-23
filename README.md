# doodle-assistant

RAG + agentic assistant for a cartoon-drawing app, built in Go as a learning
project: hexagonal architecture, `net/http` straight to Ollama, Postgres +
pgvector, no SDKs.

## Status

| Phase | Scope | State |
| --- | --- | --- |
| 0 | Setup: module, pgvector compose, preflight | done |
| 1 | Ingestion: markdown chunking, embeddings, idempotent upsert | next |
| 2 | Classic RAG: `Ask` with top-K + streaming answer | pending |
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
```

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
internal/domain/        Document, Chunk, Answer, Source, Canvas, Layer - no deps
internal/app/           use cases: IngestDocs, Ask, RunAgent
internal/ports/         Embedder, VectorStore, ChatModel, CanvasRepository, Tool
internal/adapters/      ollama, postgres, memory, http, cli
docs/                   knowledge base markdown
migrations/             versioned SQL (see migrations/README.md)
eval/                   retrieval evaluation dataset
scripts/                preflight / helpers
```

`internal/domain` and `internal/app` must never import `internal/adapters`; an
architecture test enforces this from phase 1 on.
