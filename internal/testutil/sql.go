package testutil

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

// openStdlibDB returns a *sql.DB backed by the pgx stdlib driver. goose
// requires a *sql.DB; we use pgx's stdlib bridge so the binary only
// ever pulls one Postgres driver into the build graph.
func openStdlibDB(url string) (*sql.DB, error) {
	return sql.Open("pgx", url)
}
