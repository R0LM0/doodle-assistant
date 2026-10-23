-- 0001_init.sql
--
-- Base schema for the RAG knowledge base.
--
-- Design notes:
--   * documents.source_path is the identity of a source file inside docs/.
--   * documents.content_hash is the idempotency key of ingestion: same hash,
--     nothing to do; different hash, re-chunk and re-embed.
--   * chunks.embedding is vector(768) because EMBED_MODEL=embeddinggemma.
--     A different embedding model means a new column or a full re-embed.
--   * No HNSW index yet: phase 2 measures a sequential scan first.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS documents (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_path  text        NOT NULL UNIQUE,
    title        text        NOT NULL,
    content_hash text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS documents_content_hash_idx ON documents (content_hash);

CREATE TABLE IF NOT EXISTS chunks (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id  bigint      NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    heading_path text        NOT NULL,
    position     integer     NOT NULL,
    content      text        NOT NULL,
    embedding    vector(768) NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chunks_document_position_key UNIQUE (document_id, position)
);

CREATE INDEX IF NOT EXISTS chunks_document_id_idx ON chunks (document_id);
