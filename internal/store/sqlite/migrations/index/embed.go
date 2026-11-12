// Package index exposes the frozen R1 index migrations embedded with the
// binary. The SQL files remain the physical schema source.
package index

import _ "embed"

//go:embed 001.sql
var migrationV1 string

//go:embed 002.sql
var migrationV2 string

func V1() string { return migrationV1 }
func V2() string { return migrationV2 }
