package model

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

var ErrPreviewLocation = errors.New("invalid preview origin or pathname")

// CheckLocalPreviewLocation limits the U1 path to the supported loopback
// profile. Other environments require their own compatibility gate.
func CheckLocalPreviewLocation(origin, pathname string) error {
	if err := CheckPreviewLocation(origin, pathname); err != nil {
		return err
	}
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return ErrPreviewLocation
	}
	return nil
}

// CheckPreviewLocation verifies a serialized origin and path without resolving
// or connecting to the target. The browser verifies its own canonical URL too.
func CheckPreviewLocation(origin, pathname string) error {
	if VerifyPreviewEventOrigin(origin, origin) != nil || VerifyCanonicalPreviewPathPrefix(pathname, pathname) != nil {
		return ErrPreviewLocation
	}
	u, err := url.Parse(origin)
	if err != nil || u.String() != origin {
		return ErrPreviewLocation
	}
	host := u.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" || host != ip.String() {
			return ErrPreviewLocation
		}
	} else {
		labels := strings.Split(strings.TrimSuffix(host, "."), ".")
		last := labels[len(labels)-1]
		if last == "" || strings.HasPrefix(last, "0x") || strings.Trim(last, "0123456789") == "" {
			return ErrPreviewLocation
		}
		for _, label := range labels {
			if label == "" || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
				return ErrPreviewLocation
			}
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || port != strconv.Itoa(n) ||
			u.Scheme == "http" && n == 80 || u.Scheme == "https" && n == 443 {
			return ErrPreviewLocation
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return ErrPreviewLocation
	}
	raw, err := url.ParseRequestURI(pathname)
	if err != nil || raw.IsAbs() || raw.EscapedPath() != pathname || strings.Contains(raw.Path, "\\") {
		return ErrPreviewLocation
	}
	for _, segment := range strings.Split(raw.Path, "/") {
		if segment == "." || segment == ".." {
			return ErrPreviewLocation
		}
	}
	if strings.Count(raw.Path, "/") != strings.Count(pathname, "/") {
		return ErrPreviewLocation
	}
	return nil
}
