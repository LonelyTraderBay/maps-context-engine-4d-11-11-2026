package model

// ValidUUID and ValidDigest expose the existing identifier rules to local
// adapters that do not accept a complete ScopeRequest. They grant no scope.
func ValidUUID(value string) bool   { return uuid(value) }
func ValidDigest(value string) bool { return lowerHex(value, 64) }
