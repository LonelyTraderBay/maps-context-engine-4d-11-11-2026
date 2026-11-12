package model

import (
	"encoding/json"
	"fmt"
)

type ProviderMessage interface{ providerMessage() }

func (*ProviderStart) providerMessage()  {}
func (*ProviderBatch) providerMessage()  {}
func (*ProviderDone) providerMessage()   {}
func (*ProviderCancel) providerMessage() {}

// DecodeProviderMessageJSON selects exactly one closed protocol variant. Frame
// size, sequence, job ownership, deadlines and provider trust are caller duties.
func DecodeProviderMessageJSON(data []byte, maxBytes int) (ProviderMessage, error) {
	maxBytes = providerFrameLimit(maxBytes)
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return nil, err
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case "analyze_start":
		v, err := DecodeProviderStartJSON(data, maxBytes)
		if err != nil {
			return nil, err
		}
		return &v, nil
	case "analyze_batch":
		v, err := DecodeProviderBatchJSON(data, maxBytes)
		if err != nil {
			return nil, err
		}
		return &v, nil
	case "analyze_done":
		v, err := DecodeProviderDoneJSON(data, maxBytes)
		if err != nil {
			return nil, err
		}
		return &v, nil
	case "cancel":
		v, err := DecodeProviderCancelJSON(data, maxBytes)
		if err != nil {
			return nil, err
		}
		return &v, nil
	default:
		return nil, fmt.Errorf("unknown provider message")
	}
}
