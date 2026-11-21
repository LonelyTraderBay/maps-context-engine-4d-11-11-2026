package semantic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const (
	TokenizerVersion = "unicode-words-v1"
	IndexVersion     = "cosine-memory-v1"
	MaxModelBytes    = 1 << 20
	maxVocabulary    = 1024
	maxDimensions    = 64
)

// ModelPin is provided by the authorized local owner after reviewing the
// material and its license. The retrieval package never fetches model files.
type ModelPin struct {
	ModelID, ModelVersion, License, SHA256 string
	Tokenizer, Index                       string
	Dimensions                             int
}

type vocabulary struct {
	ID         string             `json:"model_id"`
	Version    string             `json:"model_version"`
	License    string             `json:"license"`
	Dimensions int                `json:"dimensions"`
	Weights    map[string][]int16 `json:"weights"`
}

var label = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$`)

func openModel(pin ModelPin, data []byte) (vocabulary, error) {
	if len(data) == 0 || len(data) > MaxModelBytes || !label.MatchString(pin.ModelID) ||
		!label.MatchString(pin.ModelVersion) || !allowedLicense(pin.License) || !model.ValidDigest(pin.SHA256) ||
		pin.Tokenizer != TokenizerVersion || pin.Index != IndexVersion || pin.Dimensions < 1 || pin.Dimensions > maxDimensions {
		return vocabulary{}, ErrModel
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != pin.SHA256 {
		return vocabulary{}, ErrModel
	}
	if _, err := model.DecodeIdentityJSON(data, MaxModelBytes); err != nil {
		return vocabulary{}, ErrModel
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var v vocabulary
	if err := decoder.Decode(&v); err != nil || v.ID != pin.ModelID || v.Version != pin.ModelVersion ||
		v.License != pin.License || v.Dimensions != pin.Dimensions || len(v.Weights) < 1 || len(v.Weights) > maxVocabulary {
		return vocabulary{}, ErrModel
	}
	for token, weights := range v.Weights {
		if len(token) < 1 || len(token) > 64 || strings.ToLower(token) != token ||
			strings.IndexFunc(token, wordBoundary) >= 0 || len(weights) != v.Dimensions {
			return vocabulary{}, ErrModel
		}
	}
	return v, nil
}

func allowedLicense(name string) bool {
	switch name {
	case "MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "0BSD":
		return true
	}
	return false
}

func wordBoundary(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }

// embed adds the approved token weights and normalizes to unit length. An
// input with no supported tokens has no vector and uses ordinary search.
func (v vocabulary) embed(input string) []float64 {
	vector := make([]float64, v.Dimensions)
	for _, token := range strings.FieldsFunc(strings.ToLower(input), wordBoundary) {
		for i, weight := range v.Weights[token] {
			vector[i] += float64(weight)
		}
	}
	var norm float64
	for _, value := range vector {
		norm += value * value
	}
	if norm == 0 {
		return nil
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] /= norm
	}
	return vector
}
