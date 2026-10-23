// Package migrations embeds the versioned SQL files so the binary can apply
// them anywhere (local, Neon, a container) without shipping a directory.
//
// Files are applied in filename order, one transaction per file, tracked in the
// schema_migrations table by internal/adapters/postgres.Migrator.
package migrations

import "embed"

// FS holds every *.sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS
