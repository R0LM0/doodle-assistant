-- 0003_fts_hybrid.sql
--
-- Full-text leg for hybrid search (fase 8).
--
-- content_tsv is generated from content with the 'spanish' configuration on
-- purpose: the knowledge base is Spanish, the same reason embeddinggemma is the
-- default embedding model. A generated column keeps the tsvector consistent on
-- every insert without application code.

ALTER TABLE chunks ADD COLUMN IF NOT EXISTS content_tsv tsvector
  GENERATED ALWAYS AS (to_tsvector('spanish', content)) STORED;

CREATE INDEX IF NOT EXISTS chunks_content_tsv_idx ON chunks USING gin (content_tsv);
