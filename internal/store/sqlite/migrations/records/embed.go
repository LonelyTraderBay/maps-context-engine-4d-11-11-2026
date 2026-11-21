package records

import _ "embed"

//go:embed 001.sql
var v1 string

//go:embed 002.sql
var v2 string

// V1 returns the frozen base records DDL.
func V1() string { return v1 }

// V2 returns the runtime-evidence migration for an already recognized V1 store.
func V2() string { return v2 }
