package federation

import (
	"encoding/json"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// DecodeDeclaredLinks decodes owner-selected, captured bridge metadata.
// Authorization to read this declaration and the endpoints is separate.
func DecodeDeclaredLinks(source model.SourceRef, raw []byte) ([]DeclaredLink, error) {
	if len(raw) > scopeBytes || !strings.HasSuffix(source.Path, ".bridge.json") ||
		source.Range.StartByte != 0 || source.Range.EndByte != int64(len(raw)) || source.ValidateCapturedBytes(raw) != nil {
		return nil, ErrInput
	}
	if _, err := model.DecodeIdentityJSON(raw, scopeBytes); err != nil {
		return nil, ErrInput
	}
	envelope, err := linkFields(raw, "format", "links")
	if err != nil {
		return nil, err
	}
	var format string
	var items []json.RawMessage
	if json.Unmarshal(envelope["format"], &format) != nil || format != "mce4d-federated-links-v1" ||
		json.Unmarshal(envelope["links"], &items) != nil || items == nil || len(items) > 512 {
		return nil, ErrInput
	}
	seen := make(map[string]bool, len(items))
	out := make([]DeclaredLink, 0, len(items))
	for _, raw := range items {
		fields, err := linkFields(raw, "id", "from", "to")
		if err != nil {
			return nil, err
		}
		var id string
		if json.Unmarshal(fields["id"], &id) != nil || !bridgeIdentifier.MatchString(id) || seen[id] {
			return nil, ErrInput
		}
		from, err := decodeContractEndpoint(fields["from"])
		if err != nil {
			return nil, err
		}
		to, err := decodeContractEndpoint(fields["to"])
		if err != nil {
			return nil, err
		}
		seen[id] = true
		out = append(out, DeclaredLink{ID: id, From: from, To: to, Declaration: source})
	}
	return out, nil
}

func decodeContractEndpoint(raw []byte) (ContractEndpoint, error) {
	fields, err := linkFields(raw, "selector", "contract")
	if err != nil {
		return ContractEndpoint{}, err
	}
	selector, err := model.DecodeRefSelectorJSON(fields["selector"], scopeBytes)
	if err != nil {
		return ContractEndpoint{}, ErrInput
	}
	selector, _, err = flowSelector(selector)
	if err != nil {
		return ContractEndpoint{}, err
	}
	if _, err := linkFields(fields["contract"], "service_id", "environment", "protocol", "namespace", "identifier", "version"); err != nil {
		return ContractEndpoint{}, err
	}
	var key contracts.Key
	if json.Unmarshal(fields["contract"], &key) != nil || value(key.ServiceID) != *selector.ServiceID ||
		key.Environment == "" || key.Protocol == "" || key.Namespace == "" || key.Identifier == "" || key.Version == "" {
		return ContractEndpoint{}, ErrInput
	}
	return ContractEndpoint{Selector: selector, Contract: key}, nil
}

func linkFields(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != len(keys) {
		return nil, ErrInput
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return nil, ErrInput
		}
	}
	return object, nil
}
