-- 0004_fts_include_heading.sql
--
-- The vector leg embeds heading + content (Chunk.EmbeddingText), but 0003 indexed
-- content only. A question naming a heading word ("atajo", "arco") then parses to
-- an AND tsquery no chunk can satisfy, and the lexical leg misses evidence the
-- vector leg finds. Index heading_path + content so both legs see the same text.

ALTER TABLE chunks DROP COLUMN IF EXISTS content_tsv;

ALTER TABLE chunks ADD COLUMN IF NOT EXISTS content_tsv tsvector
  GENERATED ALWAYS AS (to_tsvector('spanish', heading_path || ' ' || content)) STORED;

CREATE INDEX IF NOT EXISTS chunks_content_tsv_idx ON chunks USING gin (content_tsv);
