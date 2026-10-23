# doodle-assistant

RAG + agentic assistant for a cartoon-drawing app, built in Go as a learning
project: hexagonal architecture, `net/http` straight to Ollama, Postgres +
pgvector, no SDKs.

## Status

| Phase | Scope | State |
| --- | --- | --- |
| 0 | Setup: module, pgvector compose, preflight | done |
| 1 | Ingestion: markdown chunking, embeddings, idempotent upsert | done |
| 2 | Classic RAG: `Ask` with top-K + streaming answer | done |
| 3 | Evaluation: recall@K dataset | done |
| 4 | Tool calling on an in-memory canvas | done |
| 5 | Agentic RAG: `search_docs` as a tool + evidence judge | done |
| 6 | HTTP/SSE API + Vite/TS UI | done |
| 7 | Optional: Docker image + Neon | done |

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

## Asking (phase 2)

```bash
go run ./cmd/assistant ask "¿Se puede exportar un GIF con fondo transparente?"
go run ./cmd/assistant ask -no-context "¿Se puede exportar un GIF con fondo transparente?"
```

The first run is RAG: embed the question, take the `TOP_K` closest chunks by cosine
distance through the HNSW index, and answer only from a delimited context, citing
`archivo.md > Encabezado`. The second is the control group: same model, same
question, no retrieval. Comparing both is the point of the phase: without context
the model answers about GIF in general; with context it answers about this app and
cites where it read it.

The context block is fenced and the system prompt states that context is data, not
instructions: a document that contains "ignore the rules" is content to report, not
an order to follow. Sources are printed from the retrieval result, not parsed out of
the model text, so a model that forgets to cite cannot hide the evidence.

Stage timings are printed for every answer (`embed`, `search`, `generate`). Expect
`generate` to dominate: `gemma-4-e4b` has thinking enabled, so it reasons before the
first visible token.

## Evaluation (phase 3)

```bash
# current index
go run ./cmd/assistant eval -k 1,3,5,8 -verbose

# re-index with another chunk size, then measure
CHUNK_MAX_CHARS=600 CHUNK_OVERLAP_CHARS=60 CHUNK_MIN_CHARS=100 \
  go run ./cmd/assistant ingest
go run ./cmd/assistant eval -k 1,3,5,8
```

`eval/questions.json` holds 15 questions, each naming the expected document, an
expected heading and a distinctive phrase. A retrieved chunk counts as relevant when
the document matches and either the heading or the phrase matches: chunking options
that merge a small section into its neighbour keep the evidence and lose the heading,
and the metric must not punish that.

First measurement (heading-only relevance) reported `recall@1..8 = 0.400` for
`CHUNK_MAX_CHARS=2400`, with the same value for every K. A recall that does not move
with K means the evidence never appears under the expected label, not that ranking is
bad: the merged chunks were still there. With the content fallback, the same index
scores `recall@3 = 1.000`.

Current results (`embeddinggemma`, 8 documents):

| chunk max / overlap / min | chunks | recall@1 | recall@3 | recall@8 | MRR |
| --- | --- | --- | --- | --- | --- |
| 600 / 60 / 100 | 39 | 0.867 | 1.000 | 1.000 | 0.933 |
| 1200 / 150 / 200 | 33 | 0.867 | 1.000 | 1.000 | 0.933 |
| 2400 / 240 / 400 | 14 | 0.867 | 1.000 | 1.000 | 0.933 |

The metric saturates at this corpus size: 14 to 39 chunks and K=8 means most of the
index is returned, so every configuration looks perfect. Two questions sit at rank 2
(`q04`, `q12`) and nothing worse. To discriminate chunk sizes, the corpus has to grow
(or the primary metric has to be recall@1 + MRR). Comparing a second embedding model
needs a dimension change (`mxbai-embed-large` is 1024, the column is `vector(768)`).

## Agent (phase 4)

```bash
go run ./cmd/assistant agent "Poné el crayon con jiggle 6 y velocidad 18, y mostrame las capas."
go run ./cmd/assistant agent "Exportá el proyecto a GIF en 2x con 24 fps."
go run ./cmd/assistant agent "Borrá el archivo /etc/passwd"   # no such tool
```

The canvas is in memory (`memory.Canvas`) and the tool set is four functions:
`list_layers`, `set_wiggle`, `toggle_layer_visibility`, `export_project` (the export is
simulated: it describes what would be produced).

The split is the point of the phase: **the model decides, the code executes.** Per turn
it emits `tool_calls`; the loop looks each name up in an allowlist map, decodes the
arguments strictly (`DisallowUnknownFields`), re-validates the ranges in `domain`
(jiggle 0-20, speed 1-24, fps 1-60, scale 1-2), executes, and appends the result as a
`role: tool` message before asking again. Maximum 5 iterations.

Four real runs against `gemma-4-e4b`:

| prompt | tools called | result |
| --- | --- | --- |
| set wiggle on crayon + list layers | `list_layers`, `set_wiggle` | correct order, 3 iterations |
| export GIF 2x 24fps | `export_project` | correct arguments, 2 iterations |
| jiggle 900 | `list_layers` only | refused with the documented range |
| delete `/etc/passwd` | none | refused: no such capability |

Defences, in the order they apply: the tool set is an allowlist (unknown names never
reach code), arguments are strictly decoded, ranges are validated by the domain even if
the model ignores the schema, a failing tool becomes an error result the model can react
to instead of killing the run, and the loop is bounded. Tool output is data, never
instructions.

## Agentic RAG (phase 5)

```bash
go run ./cmd/assistant ask -agentic "¿Qué limitación tiene exportar a GIF con fondo transparente?"
go run ./cmd/assistant eval -agentic -k 1,3,5,8 -verbose
go run ./cmd/assistant agent "¿Cuál es el tope de exportación? Después poné el crayon en jiggle 8."
```

Two additions to the classic pipeline:

1. `search_docs` is a tool, so the agent decides when to search. One real run used
   `list_layers`, then `search_docs`, then `set_wiggle`: it answered a documentation
   question and performed a canvas action in the same loop.
2. `EvidenceJudge` asks the model, in a separate call without tools, whether the
   retrieved fragments suffice. When they do not, `Reformulate` produces a better query
   and the search repeats (2 attempts by default, hard cap 4).

Retrieval quality on the same 15-question dataset:

| retriever | recall@1 | recall@3 | MRR |
| --- | --- | --- | --- |
| single shot (phase 2) | 0.867 | 1.000 | 0.933 |
| agentic (judge + reformulate) | 0.933 | 1.000 | 0.967 |

The gain is real but small and costs extra model calls: on this corpus the judge says
"enough" on the first attempt most of the time, so agentic retrieval mostly pays twice
for the same answer. The honest conclusion is that judge + reformulation earns its keep
when retrieval fails, and here it rarely does.

## HTTP API and UI (phase 6)

```bash
cd web && npm install && npm run build && cd ..
go run ./cmd/assistant serve          # http://localhost:8080
```

| endpoint | behaviour |
| --- | --- |
| `GET /api/health` | liveness |
| `GET /api/stats` | documents and chunks indexed |
| `POST /api/ask` | SSE stream: `delta`, `sources`, `done`, plus `agentic` metadata |
| `POST /api/agent` | SSE stream: `delta`, `tools`, `done` |

SSE is used because the answer is generated token by token and the user should see it;
`EventSource` cannot POST a JSON body, so the UI reads the stream from `fetch()` and
parses the `event:`/`data:` blocks itself. Once the headers are sent an error becomes an
`event: error` instead of an HTTP status.

The UI is Vite + TypeScript with no framework: a form with mode selector, a live region
for the streamed answer, sources as buttons that expand the exact fragment used (so a
citation is verifiable, not decorative), tool traces for the agent, visible focus rings,
a skip link, `aria-busy` while streaming, a cancel button and Ctrl+Enter to submit.

## Deployment (phase 7)

```bash
docker build -t doodle-assistant:local .
docker compose up -d --build                    # postgres + app
docker compose run --rm app migrate             # schema first
docker compose run --rm app ingest              # index the docs
```

The image is multi-stage: Node builds the UI, Go builds a static binary, and the runtime
is `distroless/static:nonroot` — no shell, no package manager, non-root by default.

Moving to Neon is a `DATABASE_URL` change; Neon's free plan includes pgvector, 1 GB of
storage and 100 CU-hours per project, with compute scaling to zero after 5 minutes.

Free hosting is the awkward part of this project, and not for the reason it looks: the
assistant calls **Ollama's HTTP API** for chat and embeddings, so a sleeping free dyno
with a local Ollama is useless. Either the embedding at ingest time already ran (vectors
live in the database) and only chat goes remote, or `OLLAMA_BASE_URL` must point to a
hosted endpoint — which also needs an auth header this adapter does not send yet. What
was verified today: Render free sleeps, Koyeb scales to zero, Railway gives a $1 credit,
Fly.io is trial-only; verify before choosing, those terms move.

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
