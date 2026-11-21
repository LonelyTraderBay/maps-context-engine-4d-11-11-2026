package contracts

import (
	"encoding/json"
	"regexp"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

type FrontendRoute struct {
	Config    string
	Path      string
	Site      model.SourceRef
	Component model.SourceRef
}

type FrontendCall struct {
	Config string
	Method string
	Path   string
	Site   model.SourceRef
}

type FrontendAnalysis struct {
	Routes  []FrontendRoute
	Clients []FrontendCall
	Gaps    []string
}

var frontendPath = regexp.MustCompile(`^/(?:[A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)?$`)

func DecodeFrontendAnalysis(raw []byte, job ports.ProviderJob) (FrontendAnalysis, error) {
	var envelope struct {
		Protocol    int               `json:"frontend_protocol"`
		JobID       string            `json:"job_id"`
		InputDigest string            `json:"input_digest"`
		Routes      []json.RawMessage `json:"routes"`
		Clients     []json.RawMessage `json:"clients"`
		Gaps        []string          `json:"gaps"`
	}
	profile, ok := job.Start.Profile.(*model.TSProfile)
	if !ok || profile == nil || job.Capture == nil || len(raw) > int(job.Start.MaxFrameBytes) {
		return FrontendAnalysis{}, ErrDomainRecord
	}
	if _, err := model.DecodeIdentityJSON(raw, maxBatchBytes); err != nil {
		return FrontendAnalysis{}, ErrDomainRecord
	}
	manifest := job.Capture.Manifest()
	if _, err := fields(raw, &envelope, "frontend_protocol", "job_id", "input_digest", "routes", "clients", "gaps"); err != nil ||
		envelope.Protocol != 1 || envelope.JobID != job.Start.JobID || envelope.InputDigest != manifest.SnapshotDigest ||
		envelope.Routes == nil || len(envelope.Routes) > 4096 || envelope.Clients == nil || len(envelope.Clients) > 4096 ||
		envelope.Gaps == nil || len(envelope.Gaps) > 64 {
		return FrontendAnalysis{}, ErrDomainRecord
	}
	configs := make(map[string]bool, len(profile.TsconfigPaths))
	for _, config := range profile.TsconfigPaths {
		configs[config] = true
	}
	out := FrontendAnalysis{Routes: make([]FrontendRoute, 0, len(envelope.Routes)),
		Clients: make([]FrontendCall, 0, len(envelope.Clients)), Gaps: envelope.Gaps}
	for _, raw := range envelope.Routes {
		var route struct {
			Config string `json:"config"`
			Path   string `json:"path"`
		}
		obj, err := fields(raw, &route, "config", "path", "site", "component")
		if err != nil || !configs[route.Config] || !validFrontendPath(route.Path) {
			return FrontendAnalysis{}, ErrDomainRecord
		}
		site, err := frontendSource(obj["site"], job, manifest.WorktreeID)
		if err != nil {
			return FrontendAnalysis{}, err
		}
		component, err := frontendSource(obj["component"], job, manifest.WorktreeID)
		if err != nil {
			return FrontendAnalysis{}, err
		}
		out.Routes = append(out.Routes, FrontendRoute{Config: route.Config, Path: route.Path, Site: site, Component: component})
	}
	for _, raw := range envelope.Clients {
		var call struct {
			Config string `json:"config"`
			Method string `json:"method"`
			Path   string `json:"path"`
		}
		obj, err := fields(raw, &call, "config", "method", "path", "site")
		if err != nil || !configs[call.Config] || !validFrontendPath(call.Path) {
			return FrontendAnalysis{}, ErrDomainRecord
		}
		switch call.Method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		default:
			return FrontendAnalysis{}, ErrDomainRecord
		}
		site, err := frontendSource(obj["site"], job, manifest.WorktreeID)
		if err != nil {
			return FrontendAnalysis{}, err
		}
		out.Clients = append(out.Clients, FrontendCall{Config: call.Config, Method: call.Method, Path: call.Path, Site: site})
	}
	for _, gap := range out.Gaps {
		switch gap {
		case "TS_CAPTURE_INCOMPLETE", "TS_CONFIG_CYCLE", "TS_CONFIG_INVALID", "TS_CONFIG_OUTSIDE_CAPTURE",
			"TS_CONFIG_PLUGINS_UNSUPPORTED", "TS_SYNTAX_ONLY", "FRONTEND_ROUTE_PROPERTIES_UNRESOLVED",
			"FRONTEND_ROUTE_UNRESOLVED", "FRONTEND_CLIENT_UNRESOLVED":
		default:
			return FrontendAnalysis{}, ErrDomainRecord
		}
	}
	return out, nil
}

func validFrontendPath(path string) bool {
	return len(path) <= 1000 && frontendPath.MatchString(path)
}

func frontendSource(raw []byte, job ports.ProviderJob, worktree string) (model.SourceRef, error) {
	var site struct {
		Path  string `json:"path"`
		Start *int64 `json:"start_byte"`
		End   *int64 `json:"end_byte"`
	}
	if _, err := fields(raw, &site, "path", "start_byte", "end_byte"); err != nil ||
		site.Start == nil || site.End == nil || *site.End <= *site.Start {
		return model.SourceRef{}, ErrDomainRecord
	}
	span := model.ByteRange{StartByte: *site.Start, EndByte: *site.End}
	digest, err := job.Capture.CheckCapturedRange(site.Path, span)
	if err != nil {
		return model.SourceRef{}, err
	}
	return model.SourceRef{WorktreeID: worktree, ProfileID: job.Start.ProfileID, GenerationID: job.Start.GenerationID,
		ContentHash: digest, Path: site.Path, Range: span}, nil
}
