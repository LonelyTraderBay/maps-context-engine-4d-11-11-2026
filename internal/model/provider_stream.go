package model

import (
	"encoding/json"
	"fmt"
)

// ProviderStreamState tracks one bounded protocol stream; it does not start a
// worker, authorize facts, publish data or prove source membership.
type ProviderStreamState struct {
	jobID       string
	inputDigest string
	next        int64
	done        bool
	status      string
}

func BeginProviderStream(jobID, inputDigest string) (ProviderStreamState, error) {
	if !uuid(jobID) || !lowerHex(inputDigest, 64) {
		return ProviderStreamState{}, fmt.Errorf("invalid provider stream identity")
	}
	return ProviderStreamState{jobID: jobID, inputDigest: inputDigest, next: 1}, nil
}

// AcceptProviderOutput enforces job, sequence, closed shape and terminal digest.
// The caller remains responsible for framing, deadline, cancellation and source checks.
func AcceptProviderOutput(state ProviderStreamState, message ProviderMessage, maxBytes int) (ProviderStreamState, error) {
	if state.done || !uuid(state.jobID) || !lowerHex(state.inputDigest, 64) || state.next < 1 || state.next > maxExactInteger || message == nil {
		return ProviderStreamState{}, fmt.Errorf("provider stream unavailable")
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return ProviderStreamState{}, err
	}
	switch message.(type) {
	case *ProviderBatch:
		batch, e := DecodeProviderBatchJSON(raw, maxBytes)
		if e != nil {
			return ProviderStreamState{}, e
		}
		if batch.JobID != state.jobID || batch.Sequence != state.next || state.next == maxExactInteger {
			return ProviderStreamState{}, fmt.Errorf("provider batch sequence mismatch")
		}
		state.next++
		return state, nil
	case *ProviderDone:
		done, e := DecodeProviderDoneJSON(raw, maxBytes)
		if e != nil {
			return ProviderStreamState{}, e
		}
		if done.JobID != state.jobID || done.Sequence != state.next {
			return ProviderStreamState{}, fmt.Errorf("provider done sequence mismatch")
		}
		if err = done.VerifyInputDigest(state.inputDigest); err != nil {
			return ProviderStreamState{}, err
		}
		state.done = true
		state.status = done.Status
		return state, nil
	default:
		return ProviderStreamState{}, fmt.Errorf("unexpected provider output")
	}
}

// TerminalStatus requires a validated Done frame and reports its actual status.
// Complete here is a protocol claim, not authority to publish source facts.
func (state ProviderStreamState) TerminalStatus() (string, error) {
	if !state.done {
		return "", fmt.Errorf("provider done missing")
	}
	return state.status, nil
}
