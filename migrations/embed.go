// Package migrations embeds the goose SQL migrations.
package migrations

import "embed"

// FS holds the *.sql migrations for goose.
//
//go:embed *.sql
var FS embed.FS
