// Package db embeds the versioned SQL migrations so the binary is self-contained.
package db

import "embed"

// Migrations holds golang-migrate files named NNNNNN_description.{up,down}.sql.
//
//go:embed migrations/*.sql
var Migrations embed.FS
