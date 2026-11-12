package model

import (
	"fmt"
	"net/url"
	"strings"
)

// VerifyPreviewEventOrigin compares an approved origin to the canonical origin
// obtained from a verified browser event. The caller must verify event.source,
// profile authorization, environment and approved path separately. It does not
// turn arbitrary user input into a browser-origin authority or fetch any URL.
func VerifyPreviewEventOrigin(approvedOrigin, browserEventOrigin string) error {
	if !boundedString(approvedOrigin, 2048) || approvedOrigin != browserEventOrigin {
		return fmt.Errorf("preview origin mismatch")
	}
	u, err := url.Parse(approvedOrigin)
	if err != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(approvedOrigin, "?#\\\r\n\t") {
		return fmt.Errorf("invalid preview origin")
	}
	return nil
}
