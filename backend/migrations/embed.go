// Package migrations embeds Den's Goose migration .sql files so they can be
// run from the cmd/migrate binary without a separate goose CLI install.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
