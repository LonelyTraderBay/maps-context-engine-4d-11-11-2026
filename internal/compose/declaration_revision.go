package compose

import (
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// DeclarationRevisionDigest binds a stable authority identity and its current
// revision. The caller must independently verify both inputs and recheck them
// when the declaration set is used; a digest alone is not authorization.
func DeclarationRevisionDigest(authorityID, revisionID string) (string, error) {
	if len(authorityID) == 0 || len(authorityID) > 768 || len(revisionID) == 0 || len(revisionID) > 768 {
		return "", app.ErrCoreInput
	}
	authority, err := model.CanonicalString(authorityID)
	if err != nil {
		return "", app.ErrCoreInput
	}
	revision, err := model.CanonicalString(revisionID)
	if err != nil {
		return "", app.ErrCoreInput
	}
	_, digest, err := model.EncodeCanonical("declaration-revision-v1", model.CanonicalArray([]model.CanonicalValue{authority, revision}), 8192)
	if err != nil {
		return "", app.ErrCoreInput
	}
	return digest, nil
}
