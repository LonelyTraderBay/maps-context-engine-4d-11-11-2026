package overview

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// DocumentDeclaration is an explicit, authorized declaration. The excerpt
// is always read from the pinned SourceRef, never supplied by the caller.
// A nonempty EntityID also requires the callback to verify that explicit link.
type DocumentDeclaration struct {
	Kind     string          `json:"kind"` // objective, constraint, component
	Source   model.SourceRef `json:"source"`
	EntityID string          `json:"entity_id,omitempty"`
}

type PlanInput struct {
	Overview     *Report
	Snapshot     *snapshot.Snapshot
	Policy       model.PolicyConfig
	Selector     model.Selector
	Declarations []DocumentDeclaration
	Scanner      *policy.Scanner
	TaskKind     string // empty or plan; explore uses the same evidence boundary
	Intent       string
	Constraints  []string
	// The owner proves that the declaration was explicitly approved as data;
	// it must recheck actor, declaration revision and policy at every call.
	AuthorizeDocument func(context.Context, Scope, DocumentDeclaration) error
	MaxBytes          int
}

type PlanItem struct {
	ID                string          `json:"id"`
	Kind              string          `json:"kind"`
	EntityID          string          `json:"entity_id,omitempty"`
	Plane             string          `json:"plane"`
	MappingStatus     string          `json:"mapping_status"`
	Source            model.SourceRef `json:"source"`
	Content           string          `json:"content"`
	RequiredForReview bool            `json:"required_for_review"`
}

// Structure items are derived, source-backed metadata. Their content is JSON
// data, not a verbatim source excerpt and not a runtime observation.
type PlanModuleStructure struct {
	Module       model.ModuleRef `json:"module"`
	ParentID     string          `json:"parent_id"`
	Root         string          `json:"root"`
	ImportPath   string          `json:"import_path"`
	ModuleSource model.SourceRef `json:"module_source"`
}

type PlanFileStructure struct {
	Module     model.ModuleRef   `json:"module"`
	Package    model.PackageFact `json:"package"`
	FileSource model.SourceRef   `json:"file_source"`
}

type PlanReport struct {
	Scope                  Scope      `json:"scope"`
	TaskKind               string     `json:"task_kind"`
	Intent                 string     `json:"intent"`
	Constraints            []string   `json:"constraints"`
	ProjectStage           string     `json:"project_stage"`
	ImplementationEvidence string     `json:"implementation_evidence"`
	ValidationStatus       string     `json:"validation_status"`
	Modules                []Module   `json:"modules"`
	Items                  []PlanItem `json:"items"`
	Gaps                   []string   `json:"gaps"`
}

type Plan struct {
	overview           *Report
	declarations       []DocumentDeclaration
	authorizeDocument  func(context.Context, Scope, DocumentDeclaration) error
	report             PlanReport
	maxBytes           int
	policyMaxOutput    int64
	snapshot           *snapshot.Snapshot
	selectorJSON       []byte
	retentionCheck     func(context.Context) error
	retentionExpires   int64
	verifyDeclarations func(context.Context, Scope, []DocumentDeclaration) error
}

// Selector returns a validated copy of the selection used to build the plan.
func (p *Plan) Selector() (model.Selector, error) {
	if p == nil || len(p.selectorJSON) == 0 {
		return nil, ErrInput
	}
	return model.DecodeSelectorJSON(append([]byte(nil), p.selectorJSON...), 16384)
}

func (p *Plan) PolicyMaxOutput() int64 {
	if p == nil {
		return 0
	}
	return p.policyMaxOutput
}

func boundedSourceRef(ref model.SourceRef) bool {
	return len(ref.WorktreeID) == 36 && len(ref.ProfileID) == 64 && len(ref.GenerationID) == 36 && len(ref.ContentHash) == 64 && len(ref.Path) <= 16384
}

func boundedPlanSelector(value model.Selector) bool {
	switch s := value.(type) {
	case *model.WorkspaceSelector:
		return s != nil && len(s.Kind) <= 32
	case *model.ModuleSelector:
		return s != nil && len(s.Kind) <= 32 && len(s.ModuleID) <= 768
	case *model.EntitySelector:
		return s != nil && len(s.Kind) <= 32 && len(s.EntityID) <= 768
	case *model.DocumentSelector:
		return s != nil && len(s.Kind) <= 32 && len(s.Path) <= 16384 && len(s.ContentHash) == 64
	case *model.RefSelector:
		if s == nil || len(s.Kind) > 32 || s.ServiceID != nil {
			return false
		}
		switch ref := s.Ref.(type) {
		case *model.ModuleRef:
			return ref != nil && len(ref.Kind) <= 32 && len(ref.WorktreeID) == 36 && len(ref.ProfileID) == 64 && len(ref.GenerationID) == 36 && len(ref.ModuleID) <= 768
		case *model.EntityRef:
			return ref != nil && len(ref.Kind) <= 32 && len(ref.WorktreeID) == 36 && len(ref.ProfileID) == 64 && len(ref.GenerationID) == 36 && len(ref.EntityID) <= 768
		default:
			return false
		}
	default:
		return false
	}
}

func boundedPlanPolicy(p model.PolicyConfig) bool {
	if len(p.AllowedRoots) > 32 || len(p.DeniedGlobs) > 256 || len(p.TrustedRulePaths) > 128 || len(p.DependencyRoots) > 128 || len(p.AllowedExecutables) > 32 || len(p.SecretRulesVersion) > 768 {
		return false
	}
	var bytes int
	for _, group := range [][]string{p.AllowedRoots, p.DeniedGlobs, p.TrustedRulePaths, p.DependencyRoots} {
		for _, value := range group {
			if len(value) > 16384 {
				return false
			}
			bytes += len(value)
			if bytes > 1024*1024 {
				return false
			}
		}
	}
	for _, exe := range p.AllowedExecutables {
		if len(exe.Path) > 16384 || len(exe.SHA256) > 64 {
			return false
		}
		bytes += len(exe.Path) + len(exe.SHA256)
		if bytes > 1024*1024 {
			return false
		}
	}
	return true
}

func (p *Plan) authorize(ctx context.Context) error {
	if p == nil || p.overview == nil || ctx == nil || p.authorizeDocument == nil {
		return ErrInput
	}
	// Retained token lifetime/current owner is checked after all private
	// callbacks below. No content is released before that final check.
	checkDeclarations := func() error {
		if p.verifyDeclarations == nil {
			return nil
		}
		declarations := append([]DocumentDeclaration{}, p.declarations...)
		if err := p.verifyDeclarations(ctx, p.overview.scope, declarations); err != nil {
			return authorityError(ctx)
		}
		return ctx.Err()
	}
	if err := checkDeclarations(); err != nil {
		return err
	}
	// The report includes structural facts and stage information derived from
	// the overview, so its contributing sources remain authorization inputs.
	paths := append([]string{}, p.overview.paths...)
	seen := make(map[string]struct{}, len(paths)+len(p.declarations))
	for _, name := range paths {
		seen[name] = struct{}{}
	}
	for _, d := range p.declarations {
		if _, ok := seen[d.Source.Path]; !ok {
			seen[d.Source.Path] = struct{}{}
			paths = append(paths, d.Source.Path)
		}
	}
	// These callbacks verify declarations held as private immutable data. The
	// full overview check below verifies every contributing path and brackets
	// them with current scope/facts before any report can leave this owner.
	for _, d := range p.declarations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.authorizeDocument(ctx, p.overview.scope, d); err != nil {
			return authorityError(ctx)
		}
	}
	if err := p.overview.authorize(ctx, paths); err != nil {
		return err
	}
	if err := checkDeclarations(); err != nil {
		return err
	}
	if p.retentionCheck != nil {
		return p.retentionCheck(ctx)
	}
	return ctx.Err()
}

// Read rechecks full authorization after copying private immutable results. An
// intended component stays unmapped until a separate supported mechanism has
// established an explicit link; no graph edge or symbol is created here.
func (p *Plan) Read(ctx context.Context) (PlanReport, error) {
	if p == nil || ctx == nil {
		return PlanReport{}, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return PlanReport{}, err
	}
	raw, err := json.Marshal(p.report)
	if err != nil || len(raw) > p.maxBytes {
		return PlanReport{}, ErrLimit
	}
	var result PlanReport
	if json.Unmarshal(raw, &result) != nil {
		return PlanReport{}, ErrInput
	}
	if err = p.authorize(ctx); err != nil {
		return PlanReport{}, err
	}
	return result, nil
}

// BuildPlan accepts only workspace, module and exact document selectors. The
// caller routes symbol selectors to the already existing symbol task slice.
func BuildPlan(ctx context.Context, in PlanInput) (*Plan, error) {
	if ctx == nil || in.Overview == nil || in.Snapshot == nil || in.Scanner == nil || !boundedPlanSelector(in.Selector) || !boundedPlanPolicy(in.Policy) || in.AuthorizeDocument == nil || in.MaxBytes < 4096 || in.MaxBytes > 16*1024*1024 || len(in.Declarations) > 512 {
		return nil, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInput
	}
	taskKind := in.TaskKind
	if taskKind == "" {
		taskKind = "plan"
	}
	if taskKind != "plan" && taskKind != "explore" {
		return nil, ErrInput
	}
	if len(in.Intent) > 8192 || !utf8.ValidString(in.Intent) || utf8.RuneCountInString(in.Intent) > 2048 || in.Scanner.CheckContent([]byte(in.Intent)) != nil || len(in.Constraints) > 16 {
		return nil, ErrInput
	}
	constraints := append([]string{}, in.Constraints...)
	for _, constraint := range constraints {
		switch constraint {
		case "preserve_json_keys", "preserve_db_schema", "no_public_api_change", "preserve_message_schema", "no_source_mutation":
		default:
			return nil, ErrInput
		}
	}
	sort.Strings(constraints)
	for i := 1; i < len(constraints); i++ {
		if constraints[i] == constraints[i-1] {
			return nil, ErrInput
		}
	}
	manifest := in.Snapshot.Manifest()
	scope := in.Overview.scope
	if manifest.WorktreeID != scope.Identity.WorktreeID || manifest.ProfileID != scope.Identity.ProfileID || manifest.SnapshotDigest != scope.SnapshotDigest || manifest.PolicyDigest != scope.PolicyDigest || model.VerifyCaptureDigest(manifest, 64*1024*1024) != nil {
		return nil, ErrInput
	}
	policyBytes, err := json.Marshal(in.Policy)
	if err != nil || len(policyBytes) > 1024*1024 {
		return nil, ErrInput
	}
	approved, err := model.DecodePolicyConfigJSON(policyBytes, 1024*1024)
	if err != nil {
		return nil, ErrInput
	}
	digest, err := model.PolicyDigest(approved, 1024*1024)
	if err != nil || digest != scope.PolicyDigest {
		return nil, ErrInput
	}
	rawSelector, err := json.Marshal(in.Selector)
	if err != nil || len(rawSelector) > 16384 {
		return nil, ErrInput
	}
	selector, err := model.DecodeSelectorJSON(rawSelector, 16384)
	if err != nil {
		return nil, ErrInput
	}
	summary, err := in.Overview.Read(ctx)
	if err != nil {
		return nil, err
	}
	selectedModules := []Module{}
	moduleRoot := ""
	entityID := ""
	var document *model.DocumentSelector
	switch s := selector.(type) {
	case *model.WorkspaceSelector:
		selectedModules = append(selectedModules, summary.Modules...)
	case *model.ModuleSelector:
		for _, m := range summary.Modules {
			if m.Ref.ModuleID == s.ModuleID {
				selectedModules = append(selectedModules, m)
				moduleRoot = m.Root
			}
		}
		if len(selectedModules) != 1 {
			return nil, ErrInput
		}
	case *model.DocumentSelector:
		document = s
	case *model.EntitySelector:
		entityID = s.EntityID
	case *model.RefSelector:
		if s.ServiceID != nil {
			return nil, ErrInput
		}
		switch ref := s.Ref.(type) {
		case *model.ModuleRef:
			if ref.GraphIdentity != scope.Identity {
				return nil, ErrInput
			}
			for _, m := range summary.Modules {
				if m.Ref.ModuleID == ref.ModuleID {
					selectedModules = append(selectedModules, m)
					moduleRoot = m.Root
				}
			}
			if len(selectedModules) != 1 {
				return nil, ErrInput
			}
		case *model.EntityRef:
			if ref.GraphIdentity != scope.Identity {
				return nil, ErrInput
			}
			entityID = ref.EntityID
		default:
			return nil, ErrInput
		}
	default:
		return nil, ErrInput
	}
	trusted := map[string]bool{}
	for _, path := range approved.TrustedRulePaths {
		trusted[path] = true
	}
	p := &Plan{overview: in.Overview, authorizeDocument: in.AuthorizeDocument, maxBytes: in.MaxBytes, policyMaxOutput: approved.MaxOutputBytes, snapshot: in.Snapshot, selectorJSON: append([]byte(nil), rawSelector...), report: PlanReport{Scope: scope, TaskKind: taskKind, Intent: in.Intent, Constraints: constraints, ValidationStatus: "not_checked", Modules: selectedModules, Items: []PlanItem{}, Gaps: append([]string{}, summary.Gaps...)}}
	usedItemBytes := 0
	appendItem := func(item PlanItem) error {
		raw, err := json.Marshal(item)
		if err != nil || len(raw)+1 > in.MaxBytes-usedItemBytes {
			return ErrLimit
		}
		usedItemBytes += len(raw) + 1
		p.report.Items = append(p.report.Items, item)
		return nil
	}
	declaredPaths := map[string]bool{}
	seen := map[string]bool{}
	scanned := map[string]snapshot.File{}
	var scannedBytes int64
	selectionMatched := false
	for _, d := range in.Declarations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if d.Kind != "objective" && d.Kind != "constraint" && d.Kind != "component" {
			return nil, ErrInput
		}
		if len(d.EntityID) > 768 || in.Scanner.CheckContent([]byte(d.EntityID)) != nil {
			return nil, ErrInput
		}
		if d.EntityID != "" {
			raw, e := json.Marshal(&model.EntityRef{Kind: "entity", GraphIdentity: scope.Identity, EntityID: d.EntityID})
			if e != nil {
				return nil, ErrInput
			}
			if _, e = model.DecodeGraphRefJSON(raw, 16384); e != nil {
				return nil, ErrInput
			}
		}
		if d.Kind == "constraint" && !trusted[d.Source.Path] {
			return nil, ErrAuthority
		}
		if !boundedSourceRef(d.Source) {
			return nil, ErrInput
		}
		refRaw, err := json.Marshal(d.Source)
		if err != nil || len(refRaw) > 16384 {
			return nil, ErrInput
		}
		ref, err := model.DecodeSourceRefJSON(refRaw, 16384)
		if err != nil || ref.WorktreeID != scope.Identity.WorktreeID || ref.ProfileID != scope.Identity.ProfileID || ref.GenerationID != scope.Identity.GenerationID || ref.Range.EndByte <= ref.Range.StartByte || ref.Range.EndByte-ref.Range.StartByte > 131072 || policy.CheckPath(ref.Path) != nil || in.Scanner.CheckContent([]byte(ref.Path)) != nil {
			return nil, ErrInput
		}
		if err := in.Overview.authorize(ctx, []string{ref.Path}); err != nil {
			return nil, err
		}
		if err := in.AuthorizeDocument(ctx, scope, d); err != nil {
			return nil, authorityError(ctx)
		}
		file, ok := scanned[ref.Path]
		if !ok {
			file, ok = in.Snapshot.File(ref.Path)
			if !ok || int64(file.Len()) > 64*1024*1024-scannedBytes || in.Scanner.CheckContent(file.Bytes()) != nil {
				return nil, ErrInput
			}
			scannedBytes += int64(file.Len())
			scanned[ref.Path] = file
		}
		if file.ContentHash() != ref.ContentHash || ref.Range.EndByte > int64(file.Len()) {
			return nil, ErrInput
		}
		body, err := file.Range(int(ref.Range.StartByte), int(ref.Range.EndByte))
		if err != nil || !utf8.Valid(body) || strings.TrimSpace(string(body)) == "" {
			return nil, ErrInput
		}
		declaredPaths[ref.Path] = true
		p.declarations = append(p.declarations, d)
		matchesSelection := (moduleRoot == "" || within(moduleRoot, ref.Path)) && (entityID == "" || d.EntityID == entityID) && (document == nil || document.Path == ref.Path && document.ContentHash == ref.ContentHash && document.Range == ref.Range)
		if matchesSelection && (document != nil || entityID != "") {
			selectionMatched = true
		}
		wholeTrustedRule := d.Kind == "constraint" && trusted[ref.Path] && ref.Range.StartByte == 0 && ref.Range.EndByte == int64(file.Len())
		if !matchesSelection && !wholeTrustedRule {
			continue
		}
		identity := struct {
			Kind     string          `json:"kind"`
			Source   model.SourceRef `json:"source"`
			EntityID string          `json:"entity_id"`
		}{d.Kind, ref, d.EntityID}
		digest, err := planCanonicalDigest("plan-declaration-v1", identity, 32768)
		if err != nil {
			return nil, ErrInput
		}
		id := "plan-" + digest
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := appendItem(PlanItem{ID: id, Kind: d.Kind, EntityID: d.EntityID, Plane: "intended", MappingStatus: "unmapped", Source: ref, Content: string(body), RequiredForReview: true}); err != nil {
			return nil, err
		}
	}
	if (document != nil || entityID != "") && !selectionMatched {
		return nil, ErrInput
	}
	for _, module := range selectedModules {
		file, ok := in.Snapshot.File(module.Source.Path)
		if !ok || file.ContentHash() != module.Source.ContentHash || module.Source.Range.EndByte > int64(file.Len()) {
			return nil, ErrInput
		}
		body, err := file.Range(int(module.Source.Range.StartByte), int(module.Source.Range.EndByte))
		if err != nil || len(body) == 0 || len(body) > 131072 || in.Scanner.CheckContent(file.Bytes()) != nil {
			return nil, ErrInput
		}
		digest, err := planCanonicalDigest("plan-observed-module-v1", module.Source, 32768)
		if err != nil {
			return nil, ErrInput
		}
		if err := appendItem(PlanItem{ID: "module-" + digest, Kind: "module", Plane: "observed_static", MappingStatus: "not_applicable", Source: module.Source, Content: string(body), RequiredForReview: false}); err != nil {
			return nil, err
		}
		moduleData := PlanModuleStructure{Module: module.Ref, ParentID: module.ParentID, Root: module.Root, ImportPath: module.ImportPath, ModuleSource: module.Source}
		item, err := planStructureItem(in.Scanner, "structure_module", module.Source, moduleData, module.Ref.ModuleID, module.ParentID, module.Root, module.ImportPath, module.Source.Path)
		if err != nil {
			return nil, err
		}
		if err := appendItem(item); err != nil {
			return nil, err
		}
		for _, pkg := range module.Packages {
			for _, name := range pkg.Files {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				file, ok := scanned[name]
				if !ok {
					file, ok = in.Snapshot.File(name)
					if !ok || int64(file.Len()) > 64*1024*1024-scannedBytes {
						return nil, ErrInput
					}
					if err := in.Scanner.CheckContent(file.Bytes()); err != nil {
						return nil, err
					}
					scannedBytes += int64(file.Len())
					scanned[name] = file
				}
				ref := model.SourceRef{WorktreeID: scope.Identity.WorktreeID, ProfileID: scope.Identity.ProfileID, GenerationID: scope.Identity.GenerationID, Path: name, ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: int64(file.Len())}}
				data := PlanFileStructure{Module: module.Ref, Package: pkg.Fact, FileSource: ref}
				item, err := planStructureItem(in.Scanner, "structure_file", ref, data, module.Ref.ModuleID, pkg.Fact.PackageKey, pkg.Fact.ImportPath, pkg.Fact.ModuleKey, pkg.Fact.Variant, pkg.Fact.SemanticStatus, name)
				if err != nil {
					return nil, err
				}
				if err := appendItem(item); err != nil {
					return nil, err
				}
			}
		}
	}
	sort.Slice(p.report.Items, func(i, j int) bool {
		a, b := p.report.Items[i], p.report.Items[j]
		if a.RequiredForReview != b.RequiredForReview {
			return a.RequiredForReview
		}
		if a.Source.Path != b.Source.Path {
			return a.Source.Path < b.Source.Path
		}
		if a.Source.Range.StartByte != b.Source.Range.StartByte {
			return a.Source.Range.StartByte < b.Source.Range.StartByte
		}
		if a.Source.Range.EndByte != b.Source.Range.EndByte {
			return a.Source.Range.EndByte < b.Source.Range.EndByte
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	observed := false
	for _, module := range summary.Modules {
		for _, pkg := range module.Packages {
			if len(pkg.Files) > 0 {
				observed = true
			}
		}
	}
	allDeclared := len(manifest.Files) > 0
	for _, file := range manifest.Files {
		name := strings.ToLower(file.Path)
		documentFile := strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".rst") || strings.HasSuffix(name, ".adoc")
		if file.Status != "captured" || !declaredPaths[file.Path] || !documentFile {
			allDeclared = false
			break
		}
	}
	switch {
	case observed && len(p.declarations) > 0:
		p.report.ProjectStage = "mixed"
		p.report.ImplementationEvidence = "partial"
	case !observed && len(p.declarations) > 0 && allDeclared:
		p.report.ProjectStage = "spec_only"
		p.report.ImplementationEvidence = "none"
	case observed && len(p.declarations) == 0 && summary.UnavailableFiles == 0:
		p.report.ProjectStage = "implemented_scope"
		p.report.ImplementationEvidence = "observed"
	default:
		p.report.ProjectStage = "unknown"
		p.report.ImplementationEvidence = "partial"
	}
	if len(p.declarations) > 0 {
		p.report.Gaps = append(p.report.Gaps, "INTENDED_MAPPING_UNKNOWN")
	}
	if !allDeclared && !observed {
		p.report.Gaps = append(p.report.Gaps, "PROJECT_STAGE_UNKNOWN")
	}
	sort.Strings(p.report.Gaps)
	uniqueGaps := p.report.Gaps[:0]
	for _, gap := range p.report.Gaps {
		if len(uniqueGaps) == 0 || uniqueGaps[len(uniqueGaps)-1] != gap {
			uniqueGaps = append(uniqueGaps, gap)
		}
	}
	p.report.Gaps = uniqueGaps
	if _, err = p.Read(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func planStructureItem(scanner *policy.Scanner, kind string, source model.SourceRef, value any, scalars ...string) (PlanItem, error) {
	for _, scalar := range scalars {
		if err := scanner.CheckContent([]byte(scalar)); err != nil {
			return PlanItem{}, err
		}
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 32768 {
		return PlanItem{}, ErrLimit
	}
	if err := scanner.CheckContent(raw); err != nil {
		return PlanItem{}, err
	}
	digest, err := planCanonicalDigest("plan-structure-v1", value, 32768)
	if err != nil {
		return PlanItem{}, err
	}
	return PlanItem{ID: "structure-" + digest, Kind: kind, Plane: "observed_static", MappingStatus: "not_applicable", Source: source, Content: string(raw)}, nil
}

// planCanonicalDigest converts an already validated typed value into the
// domain-separated canonical identity recipe, never hashing JSON bytes.
func planCanonicalDigest(domain string, value any, maxBytes int) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maxBytes {
		return "", ErrLimit
	}
	canonical, err := model.DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", ErrInput
	}
	_, digest, err := model.EncodeCanonical(domain, canonical, maxBytes*2)
	if err != nil {
		return "", ErrLimit
	}
	return digest, nil
}
