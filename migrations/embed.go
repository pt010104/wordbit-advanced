package migrations

import "embed"

// FS exposes SQL migration files for runtime and tests.
//
//go:embed *.sql
var FS embed.FS
