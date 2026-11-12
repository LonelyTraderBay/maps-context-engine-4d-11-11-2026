package golang

import "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"

// These internal IDs are scoped by generation in storage. Their preimages use
// length-delimited canonical values, including variant, owner and byte range.
func identityHash(domain, packageKey, variant, path, kind, owner string, start, end int64) (string, error) {
	values := make([]model.CanonicalValue, 0, 8)
	for _, item := range []string{"go", packageKey, variant, path, kind, owner} {
		value, err := model.CanonicalString(item)
		if err != nil {
			return "", err
		}
		values = append(values, value)
	}
	first, err := model.CanonicalInt(start)
	if err != nil {
		return "", err
	}
	last, err := model.CanonicalInt(end)
	if err != nil {
		return "", err
	}
	values = append(values, first, last)
	_, digest, err := model.EncodeCanonical(domain, model.CanonicalArray(values), 32768)
	return digest, err
}
