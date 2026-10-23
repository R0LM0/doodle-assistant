//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/r0lm0/doodle-assistant/internal/adapters/postgres"
	"github.com/r0lm0/doodle-assistant/internal/domain"
	"github.com/r0lm0/doodle-assistant/migrations"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	migrator, err := postgres.NewMigrator(pool, migrations.FS)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	if _, err := migrator.Apply(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return pool
}

func vec(dim int, value float32) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = value
	}
	return v
}

func TestMigratorIsIdempotent(t *testing.T) {
	pool := testPool(t)

	migrator, err := postgres.NewMigrator(pool, migrations.FS)
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}

	applied, err := migrator.Apply(context.Background())
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("migrations applied twice: %v", applied)
	}
}

func TestSaveDocumentReplacesChunksInOneTransaction(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	sourcePath := fmt.Sprintf("integration/test-%d.md", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM documents WHERE source_path = $1`, sourcePath)
	})

	first := domain.Document{
		SourcePath:  sourcePath,
		Title:       "Integracion",
		ContentHash: "hash-v1",
		Chunks: []domain.Chunk{
			{HeadingPath: "Doc > A", Position: 0, Content: "contenido A", Embedding: vec(768, 0.1)},
			{HeadingPath: "Doc > B", Position: 1, Content: "contenido B", Embedding: vec(768, 0.2)},
		},
	}

	id, err := store.SaveDocument(ctx, first)
	if err != nil {
		t.Fatalf("first SaveDocument: %v", err)
	}

	hash, found, err := store.DocumentHash(ctx, sourcePath)
	if err != nil || !found || hash != "hash-v1" {
		t.Fatalf("hash = %q found = %v err = %v", hash, found, err)
	}

	second := first
	second.ContentHash = "hash-v2"
	second.Chunks = []domain.Chunk{
		{HeadingPath: "Doc > C", Position: 0, Content: "unico chunk nuevo", Embedding: vec(768, 0.3)},
	}

	id2, err := store.SaveDocument(ctx, second)
	if err != nil {
		t.Fatalf("second SaveDocument: %v", err)
	}
	if id2 != id {
		t.Errorf("document id changed from %d to %d; the upsert should reuse it", id, id2)
	}

	var (
		chunkCount int
		positions  []int
	)
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE document_id = $1`, id).Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 1 {
		t.Fatalf("stored chunks = %d, want 1 (old chunks must be deleted)", chunkCount)
	}

	rows, err := pool.Query(ctx, `SELECT position FROM chunks WHERE document_id = $1 ORDER BY position`, id)
	if err != nil {
		t.Fatalf("query positions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan position: %v", err)
		}
		positions = append(positions, p)
	}
	if len(positions) != 1 || positions[0] != 0 {
		t.Errorf("positions = %v, want [0]", positions)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Documents == 0 || stats.Chunks == 0 {
		t.Errorf("stats look empty: %+v", stats)
	}
}

func TestSaveDocumentEnforcesEmbeddingDimension(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	sourcePath := fmt.Sprintf("integration/dim-%d.md", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM documents WHERE source_path = $1`, sourcePath)
	})

	_, err = store.SaveDocument(ctx, domain.Document{
		SourcePath:  sourcePath,
		Title:       "Dimension incorrecta",
		ContentHash: "hash",
		Chunks: []domain.Chunk{
			{HeadingPath: "Doc", Position: 0, Content: "chunk", Embedding: vec(1024, 0.1)},
		},
	})
	if err == nil {
		t.Fatal("expected the database to reject a 1024-dim vector in a vector(768) column")
	}
	if !strings.Contains(err.Error(), "dimensions") {
		t.Errorf("unexpected error: %v", err)
	}

	// A failed save must leave nothing behind.
	_, found, err := store.DocumentHash(ctx, sourcePath)
	if err != nil {
		t.Fatalf("DocumentHash: %v", err)
	}
	if found {
		t.Error("a rejected document was persisted; the transaction did not roll back")
	}
}
