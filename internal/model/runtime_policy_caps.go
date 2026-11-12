package model

// EffectiveCoreCaps computes resource caps that cannot exceed runtime or policy.
// This arithmetic does not authorize paths, grant execution or persist config.
type EffectiveCoreCaps struct {
	MaxFileBytes              int64
	MaxSourceBytesPerSnapshot int64
	MaxOutputBytes            int64
}

func RuntimePolicyCaps(config RuntimeConfig, policy PolicyConfig) (EffectiveCoreCaps, error) {
	if err := validateRuntimeConfig(config); err != nil {
		return EffectiveCoreCaps{}, err
	}
	if _, err := policy.normalized(); err != nil {
		return EffectiveCoreCaps{}, err
	}
	source := min(config.Core.MaxSourceBytesPerSnapshot, policy.MaxSourceBytes)
	file := min(config.Core.MaxFileBytes, policy.MaxFileBytes, source)
	output := min(config.Core.MaxOutputBytes, policy.MaxOutputBytes, int64(131072))
	return EffectiveCoreCaps{MaxFileBytes: file, MaxSourceBytesPerSnapshot: source, MaxOutputBytes: output}, nil
}
