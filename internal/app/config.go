// Package app contains administrative use cases, without a transport or an
// implicit grant of access to any workspace or state path.
package app

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var ErrInvalidConfigAction = errors.New("invalid runtime config action")

const configLimit = 1 << 20

// RuntimeConfigService keeps the startup effective digest immutable. The
// caller owns and authorizes the configuration store for the service lifetime.
type RuntimeConfigService struct {
	store      ports.RuntimeConfigStore
	cpus       int
	bootConfig model.RuntimeConfig
	bootDigest string
}

// RuntimeConfigSetResult distinguishes a committed configuration from an
// uncertain or uncommitted operation; callers must not blindly retry unknown.
type RuntimeConfigSetResult struct {
	State string
	Data  model.RuntimeConfigData
}

// NewRuntimeConfigService receives an already-authorized store. Selecting a
// default or approved local configuration file belongs to the adapter caller.
func NewRuntimeConfigService(ctx context.Context, store ports.RuntimeConfigStore, availableCPUs int) (*RuntimeConfigService, error) {
	if ctx == nil || store == nil {
		return nil, ErrInvalidConfigAction
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config, digest, err := readEffectiveConfig(ctx, store, availableCPUs)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &RuntimeConfigService{store: store, cpus: availableCPUs, bootConfig: config, bootDigest: digest}, nil
}

// StartupConfig returns the immutable effective configuration captured at
// service creation; Show reports the separate on-disk configuration.
func (s *RuntimeConfigService) StartupConfig() (model.RuntimeConfig, string, error) {
	if s == nil {
		return model.RuntimeConfig{}, "", ErrInvalidConfigAction
	}
	return s.bootConfig, s.bootDigest, nil
}

// Show reports the effective on-disk config and whether it differs from the
// startup digest. It performs no writes and does not hot-reload the process.
func (s *RuntimeConfigService) Show(ctx context.Context) (model.RuntimeConfigData, error) {
	if s == nil || ctx == nil {
		return model.RuntimeConfigData{}, ErrInvalidConfigAction
	}
	if err := ctx.Err(); err != nil {
		return model.RuntimeConfigData{}, err
	}
	config, digest, err := readEffectiveConfig(ctx, s.store, s.cpus)
	if err != nil {
		return model.RuntimeConfigData{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.RuntimeConfigData{}, err
	}
	return model.RuntimeConfigData{Operation: "runtime_config", Config: config, ConfigDigest: digest, RestartRequired: digest != s.bootDigest}, nil
}

// Validate checks a validate request and computes its effective digest without
// reading or writing the state root. It is not authorization or persistence.
func (s *RuntimeConfigService) Validate(ctx context.Context, raw []byte) (model.RuntimeConfigData, error) {
	if s == nil || ctx == nil {
		return model.RuntimeConfigData{}, ErrInvalidConfigAction
	}
	config, digest, err := ValidateRuntimeConfig(ctx, raw, s.cpus)
	if err != nil {
		return model.RuntimeConfigData{}, err
	}
	return model.RuntimeConfigData{Operation: "runtime_config", Config: config, ConfigDigest: digest, RestartRequired: digest != s.bootDigest}, nil
}

// Set persists a validated config through the OS adapter's bounded lock and
// atomic CAS. The caller authenticates the administrator and authorizes the
// state root. A committed result does not hot-reload this process.
func (s *RuntimeConfigService) Set(ctx context.Context, raw []byte) (RuntimeConfigSetResult, error) {
	if s == nil || ctx == nil {
		return RuntimeConfigSetResult{State: "not_applied"}, ErrInvalidConfigAction
	}
	result, err := s.store.Set(ctx, raw, s.cpus)
	output := RuntimeConfigSetResult{State: result.State}
	if result.State == "applied" {
		output.Data = model.RuntimeConfigData{Operation: "runtime_config", Config: result.Config, ConfigDigest: result.Digest, RestartRequired: result.Digest != s.bootDigest}
	}
	return output, err
}

// ValidateRuntimeConfig validates a request without opening any state root.
// It does not persist configuration or determine authorization.
func ValidateRuntimeConfig(ctx context.Context, raw []byte, availableCPUs int) (model.RuntimeConfig, string, error) {
	if ctx == nil {
		return model.RuntimeConfig{}, "", ErrInvalidConfigAction
	}
	if err := ctx.Err(); err != nil {
		return model.RuntimeConfig{}, "", err
	}
	request, err := model.DecodeRuntimeConfigRequestJSON(raw, configLimit, availableCPUs)
	if err != nil {
		return model.RuntimeConfig{}, "", err
	}
	if request.Action != "validate" || request.Effective == nil {
		return model.RuntimeConfig{}, "", ErrInvalidConfigAction
	}
	digest, err := model.RuntimeConfigDigest(*request.Effective, configLimit)
	if err != nil {
		return model.RuntimeConfig{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return model.RuntimeConfig{}, "", err
	}
	return *request.Effective, digest, nil
}

func readEffectiveConfig(ctx context.Context, store ports.RuntimeConfigStore, cpus int) (model.RuntimeConfig, string, error) {
	raw, present, err := store.Read(ctx, configLimit)
	if err != nil {
		return model.RuntimeConfig{}, "", err
	}
	if !present {
		raw = []byte(`{"schema_version":1}`)
	}
	config, err := model.DecodeRuntimeConfigJSON(raw, configLimit, cpus)
	if err != nil {
		return model.RuntimeConfig{}, "", err
	}
	digest, err := model.RuntimeConfigDigest(config, configLimit)
	if err != nil {
		return model.RuntimeConfig{}, "", err
	}
	return config, digest, nil
}
