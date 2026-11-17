package cli

// RegisteredRepo is an internal adapter result for the existing human-readable
// repo add command. It is not a new core/MCP JSON operation or access grant.
type RegisteredRepo struct{ WorktreeID, ProfileID string }
