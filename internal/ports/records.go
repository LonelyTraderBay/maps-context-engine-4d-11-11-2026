package ports

import "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"

// StoredRecord is untrusted durable metadata until the record service checks
// current authority and provenance against its owner.
type StoredRecord struct {
	RecordID       string
	Version        int64
	Kind           string
	DeclaredStatus string
	SourceRefs     []model.SourceRef
	UIMapping      *model.UIMappingPayload
}
