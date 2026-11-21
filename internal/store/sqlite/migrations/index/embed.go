// Package index exposes the sealed index migrations. The SQL files remain
// the physical schema source.
package index

import _ "embed"

//go:embed 001.sql
var migrationV1 string

//go:embed 002.sql
var migrationV2 string

//go:embed 003.sql
var migrationV3 string

func V1() string { return migrationV1 }
func V2() string { return migrationV2 }
func V3() string { return migrationV3 }
