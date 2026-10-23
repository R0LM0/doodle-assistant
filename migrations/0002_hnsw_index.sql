-- 0002_hnsw_index.sql
--
-- Approximate nearest neighbour index over chunk embeddings.
--
-- vector_cosine_ops matches the "<=>" operator used by Search, so the planner can
-- use the index instead of a sequential scan. HNSW is approximate: it trades a
-- tiny recall loss for orders of magnitude fewer distance computations.
--
--   m                 = 16  neighbours per node (graph connectivity)
--   ef_construction   = 64  candidate list while building (quality vs build time)
--
-- Query-time recall is tuned separately with hnsw.ef_search.

CREATE INDEX IF NOT EXISTS chunks_embedding_hnsw_idx
    ON chunks USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
