package contracts

import (
	"fmt"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// The registry response schema permits a union for some operations. These
// labels bind each transport endpoint to the specific wire operation.
var responseOperation = map[string]string{
	"mce4d_workspace_status": "status", "mce4d_locate": "locate", "mce4d_context_pack": "context_pack", "mce4d_expand_pack": "expand_pack", "mce4d_validate_pack": "validate_pack", "mce4d_rename_preview": "rename_preview",
	"POST /session/bootstrap": "bootstrap", "GET /workspace": "status", "POST /graph/query": "graph_query", "POST /source/read": "source_read", "POST /mapping/resolve": "mapping_resolve", "POST /flow/query": "flow_query", "POST /context/pack": "context_pack",
	"POST /preview/attach": "preview_attach", "POST /preview/bind": "preview_bind", "POST /preview/mode": "preview_mode", "POST /preview/ack": "preview_ack", "POST /preview/invalidate": "preview_invalidate", "POST /preview/detach": "preview_detach", "POST /preview/status": "preview_status",
	"POST /mapping/propose": "mapping_propose", "POST /mapping/review": "mapping_review", "POST /records/list": "records_list", "POST /records/read": "records_read",
	"CLI preview-permission": "preview_permission_admin", "CLI group": "group_admin", "CLI config": "runtime_config", "CLI records list": "records_list", "CLI records read": "records_read", "CLI profile set": "profile_admin", "CLI policy set": "policy_admin",
}

// DecodeResponse checks endpoint/transport binding as well as the wire model.
// Failure payloads are denied until an operation contract explicitly permits them.
func (r *Registry) DecodeResponse(transport Transport, name string, data []byte, maxBytes, cpus int) (model.Response, error) {
	op, err := r.Lookup(transport, name)
	if err != nil {
		return model.Response{}, err
	}
	if maxBytes > op.responseHardLimit() {
		maxBytes = op.responseHardLimit()
	}
	label, ok := responseOperation[name]
	if !ok {
		return model.Response{}, fmt.Errorf("missing response operation binding")
	}
	result, err := model.DecodeResponseJSON(data, maxBytes, cpus, label, false)
	if err != nil {
		return model.Response{}, err
	}
	if result.Budget.MaxOutputBytes > int64(op.responseHardLimit()) {
		return model.Response{}, fmt.Errorf("response budget exceeds operation hard cap")
	}
	return result, nil
}
