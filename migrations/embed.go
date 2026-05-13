// Package migrations exposes the SQL migration files as an embedded FS so
// that goose can apply them without needing access to the on-disk migrations
// directory. This is the source of truth for what runs at api startup
// (cmd/server) and what the operator CLI (cmd/migrate) sees.
//
// New migrations are picked up automatically by the //go:embed directive
// at compile time — drop a new file in this directory matching the
// 000N_description.sql pattern and rebuild.
package migrations

import "embed"

// FS is the read-only filesystem of all baseline + incremental migrations.
// Goose's documented usage is: goose.SetBaseFS(migrations.FS) followed by
// goose.UpContext(ctx, db, "."). The "." path is relative to FS, which
// after //go:embed *.sql contains the .sql files at its root.
//
//go:embed *.sql
var FS embed.FS
