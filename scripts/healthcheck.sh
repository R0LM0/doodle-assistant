#!/usr/bin/env bash
# Preflight for the doodle-assistant stack: proves every local dependency the
# assistant needs (Postgres + pgvector, Ollama, both models) is actually up.
#
# Usage: scripts/healthcheck.sh   (Git Bash / WSL / Linux / macOS)
set -euo pipefail

OLLAMA_BASE_URL="${OLLAMA_BASE_URL:-http://localhost:11434}"
CHAT_MODEL="${CHAT_MODEL:-gemma-4-e4b}"
EMBED_MODEL="${EMBED_MODEL:-embeddinggemma}"
EXPECTED_DIM="${EMBED_DIM:-768}"
PG_CONTAINER="${PG_CONTAINER:-doodle-postgres}"
PG_USER="${POSTGRES_USER:-doodle}"
PG_DB="${POSTGRES_DB:-doodle}"

ok()   { printf 'OK    %s\n' "$*"; }
fail() { printf 'FAIL  %s\n' "$*" >&2; exit 1; }
warn() { printf 'WARN  %s\n' "$*"; }

show_model() { curl -sf -m 10 -H 'Content-Type: application/json' \
                 -d "{\"model\":\"$1\"}" "$OLLAMA_BASE_URL/api/show"; }

# --- 1. Postgres container health ----------------------------------------
health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$PG_CONTAINER" 2>/dev/null || echo absent)"
[ "$health" = "healthy" ] || fail "container $PG_CONTAINER health=$health (run: docker compose up -d)"
ok "postgres container healthy"

psql_q() { docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -tAc "$1"; }

# --- 2. pgvector extension ------------------------------------------------
vector_version="$(psql_q "select extversion from pg_extension where extname = 'vector'" | tr -d '[:space:]')"
if [ -z "$vector_version" ]; then
  fail "pgvector extension missing (apply migrations/0001_init.sql, or: CREATE EXTENSION vector)"
fi
ok "pgvector $vector_version"

# --- 3. Ollama reachable --------------------------------------------------
curl -sf -m 5 "$OLLAMA_BASE_URL/api/tags" >/dev/null || fail "Ollama unreachable at $OLLAMA_BASE_URL"
ok "ollama reachable at $OLLAMA_BASE_URL"

# --- 4. Both models installed --------------------------------------------
show_model "$EMBED_MODEL" >/dev/null || fail "embedding model '$EMBED_MODEL' missing (ollama pull $EMBED_MODEL)"
ok "embedding model installed: $EMBED_MODEL"

chat_show="$(show_model "$CHAT_MODEL")" || fail "chat model '$CHAT_MODEL' missing (ollama pull $CHAT_MODEL)"
ok "chat model installed: $CHAT_MODEL"

# --- 5. Tools capability (gates phase 4) ---------------------------------
if printf '%s' "$chat_show" | grep -q '"tools"'; then
  ok "chat model reports the tools capability"
else
  warn "chat model does NOT report the tools capability; phase 4 tool calling will not work"
fi

# --- 6. Embedding dimension matches vector(N) ----------------------------
dim="$(curl -sf -m 60 -H 'Content-Type: application/json' \
        -d "{\"model\":\"$EMBED_MODEL\",\"input\":\"dimension probe\"}" \
        "$OLLAMA_BASE_URL/api/embed" \
      | tr -d '\n' | sed -n 's/.*"embeddings":\[\[\([^]]*\)\]\].*/\1/p' | awk -F, '{print NF}')"
[ -n "$dim" ] || fail "could not read embedding dimension from /api/embed"
[ "$dim" = "$EXPECTED_DIM" ] || fail "embedding dim is $dim but EMBED_DIM/vector(N) expects $EXPECTED_DIM"
ok "embedding dimension $dim matches EMBED_DIM"

printf '\nAll prerequisites satisfied.\n'
