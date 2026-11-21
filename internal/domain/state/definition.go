// Package state reads bounded declarative machine definitions.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type Definition struct {
	Format      string       `json:"format"`
	MachineID   string       `json:"machine_id"`
	Version     int64        `json:"version"`
	Initial     string       `json:"initial"`
	States      []State      `json:"states"`
	Events      []string     `json:"events"`
	Guards      []Guard      `json:"guards"`
	Actions     []Action     `json:"actions"`
	Transitions []Transition `json:"transitions"`
	Invariants  []Invariant  `json:"invariants"`
	Vocabulary  []Term       `json:"vocabulary"`
	Obligations []Obligation `json:"obligations"`
}

type State struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Parent  *string  `json:"parent"`
	Initial *string  `json:"initial"`
	Entry   []string `json:"entry"`
	Exit    []string `json:"exit"`
}

type Guard struct {
	ID         string `json:"id"`
	Expression string `json:"expression"`
}

type Action struct {
	ID     string `json:"id"`
	Effect string `json:"effect"`
}

type Transition struct {
	ID          string   `json:"id"`
	From        string   `json:"from"`
	To          string   `json:"to"`
	Event       string   `json:"event"`
	Guard       *string  `json:"guard"`
	Actions     []string `json:"actions"`
	Handling    string   `json:"handling"`
	TimeoutMS   *int64   `json:"timeout_ms"`
	MaxAttempts *int64   `json:"max_attempts"`
}

type Invariant struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Guard string `json:"guard"`
}

type Term struct {
	ID      string `json:"id"`
	Meaning string `json:"meaning"`
}

type Obligation struct {
	ID         string  `json:"id"`
	Transition string  `json:"transition"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	Reason     *string `json:"reason"`
}

const Format = "mce4d-state-model-v1"

var ErrStructure = errors.New("invalid state model structure")

func EligibleSource(ref model.SourceRef) bool {
	return strings.HasSuffix(ref.Path, ".machine.json") && ref.Range.StartByte == 0 &&
		ref.Range.EndByte > 0 && ref.Range.EndByte <= contracts.StateModelMaxBytes
}

// DecodeCaptured binds the declaration to the complete captured file.
func DecodeCaptured(ref model.SourceRef, raw []byte) (Definition, error) {
	if !EligibleSource(ref) || ref.Range.EndByte != int64(len(raw)) || ref.ValidateCapturedBytes(raw) != nil {
		return Definition{}, ErrStructure
	}
	return Decode(raw)
}

// Decode parses a declaration. Guards, actions and assertions remain source
// data. Approval and runtime reachability require separate evidence.
func Decode(raw []byte) (Definition, error) {
	if err := contracts.ValidateStateModelSource(raw); err != nil {
		return Definition{}, err
	}
	var def Definition
	if err := json.Unmarshal(raw, &def); err != nil {
		return Definition{}, contracts.ErrStateModelSchema
	}
	if err := def.validate(); err != nil {
		return Definition{}, err
	}
	return def, nil
}

func indexByID[T any](items []T, key func(T) string) (map[string]T, error) {
	index := make(map[string]T, len(items))
	for _, item := range items {
		id := key(item)
		if _, exists := index[id]; exists {
			return nil, fmt.Errorf("%w: duplicate identifier", ErrStructure)
		}
		index[id] = item
	}
	return index, nil
}

func has[T any](items map[string]T, id string) bool {
	_, exists := items[id]
	return exists
}

func hasActions(items map[string]Action, ids []string) bool {
	for _, id := range ids {
		if !has(items, id) {
			return false
		}
	}
	return true
}

func (def Definition) validate() error {
	states, err := indexByID(def.States, func(item State) string { return item.ID })
	if err != nil {
		return err
	}
	events, err := indexByID(def.Events, func(id string) string { return id })
	if err != nil {
		return err
	}
	guards, err := indexByID(def.Guards, func(item Guard) string { return item.ID })
	if err != nil {
		return err
	}
	actions, err := indexByID(def.Actions, func(item Action) string { return item.ID })
	if err != nil {
		return err
	}
	transitions, err := indexByID(def.Transitions, func(item Transition) string { return item.ID })
	if err != nil {
		return err
	}
	if !has(states, def.Initial) || states[def.Initial].Parent != nil {
		return fmt.Errorf("%w: invalid root initial state", ErrStructure)
	}
	for _, item := range def.States {
		if err := validateState(item, states, actions); err != nil {
			return err
		}
	}
	for _, item := range def.Transitions {
		if !has(states, item.From) || states[item.From].Kind == "final" || !has(states, item.To) ||
			!has(events, item.Event) || (item.Guard != nil && !has(guards, *item.Guard)) || !hasActions(actions, item.Actions) {
			return fmt.Errorf("%w: unresolved transition member or outgoing transition from final state", ErrStructure)
		}
		if (item.Handling == "timeout") != (item.TimeoutMS != nil) ||
			(item.Handling == "retry") != (item.MaxAttempts != nil) ||
			(item.Handling == "compensation" && len(item.Actions) == 0) {
			return fmt.Errorf("%w: inconsistent declared failure handling", ErrStructure)
		}
	}
	if _, err := indexByID(def.Invariants, func(item Invariant) string { return item.ID }); err != nil {
		return err
	}
	for _, item := range def.Invariants {
		if !has(states, item.State) || !has(guards, item.Guard) {
			return fmt.Errorf("%w: unresolved invariant member", ErrStructure)
		}
	}
	if _, err := indexByID(def.Vocabulary, func(item Term) string { return item.ID }); err != nil {
		return err
	}
	if _, err := indexByID(def.Obligations, func(item Obligation) string { return item.ID }); err != nil {
		return err
	}
	for _, item := range def.Obligations {
		if !has(transitions, item.Transition) || (item.Status == "not_applicable_with_reason") != (item.Reason != nil) {
			return fmt.Errorf("%w: inconsistent declared failure obligation", ErrStructure)
		}
	}
	return nil
}

func validateState(item State, states map[string]State, actions map[string]Action) error {
	if !hasActions(actions, item.Entry) || !hasActions(actions, item.Exit) {
		return fmt.Errorf("%w: unresolved state hook", ErrStructure)
	}
	if (item.Kind == "compound") != (item.Initial != nil) {
		return fmt.Errorf("%w: missing or unexpected child initial state", ErrStructure)
	}
	if item.Initial != nil {
		child := states[*item.Initial]
		if child.Parent == nil || *child.Parent != item.ID {
			return fmt.Errorf("%w: initial state must be a direct child", ErrStructure)
		}
	}
	for depth, parent := 0, item.Parent; parent != nil; depth++ {
		next := states[*parent]
		if depth == 16 || next.ID == item.ID || next.Kind != "compound" {
			return fmt.Errorf("%w: invalid or cyclic state hierarchy", ErrStructure)
		}
		parent = next.Parent
	}
	return nil
}
