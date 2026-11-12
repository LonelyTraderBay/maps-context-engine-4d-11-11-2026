// Package catalog exposes the immutable catalog migration embedded with the
// binary. The SQL file remains the single physical schema source.
package catalog

import _ "embed"

//go:embed 001.sql
var migrationV1 string

func V1() string { return migrationV1 }
