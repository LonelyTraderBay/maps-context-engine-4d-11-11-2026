package records

import _ "embed"

//go:embed 001.sql
var v1 string

// V1 returns the frozen records DDL; mutation services remain a later task.
func V1() string { return v1 }
