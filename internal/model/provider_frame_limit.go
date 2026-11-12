package model

const providerFrameHardLimit = 16 * 1024 * 1024

func providerFrameLimit(maxBytes int) int {
	if maxBytes > providerFrameHardLimit {
		return providerFrameHardLimit
	}
	return maxBytes
}
