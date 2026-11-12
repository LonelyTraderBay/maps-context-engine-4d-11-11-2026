package contracts

import "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"

// DecodeRequest verifies the operation's registered transport and request shape;
// it does not dispatch the operation or provide runtime authorization.
func (r *Registry) DecodeRequest(transport Transport, name string, data []byte, maxBytes, cpus int) (model.Request, error) {
	op, err := r.Lookup(transport, name)
	if err != nil {
		return nil, err
	}
	if maxBytes > op.RequestHardLimit() {
		maxBytes = op.RequestHardLimit()
	}
	return model.DecodeRequestJSON(op.Request, data, maxBytes, cpus)
}
