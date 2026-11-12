package contracts

import (
	"fmt"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// RequestDigest binds a request to the registry operation and a principal
// supplied by the authenticated caller. It does not authenticate the principal.
func (r *Registry) RequestDigest(transport Transport, name, principalID string, data []byte, maxBytes, cpus int) (string, error) {
	op, err := r.Lookup(transport, name)
	if err != nil {
		return "", err
	}
	label, ok := responseOperation[name]
	if !ok {
		return "", fmt.Errorf("missing operation binding")
	}
	request, err := r.DecodeRequest(transport, name, data, maxBytes, cpus)
	if err != nil {
		return "", err
	}
	return model.RequestDigest(label, principalID, op.Request, request, maxBytes, cpus)
}
