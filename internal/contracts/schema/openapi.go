package schema

import (
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
)

var (
	oasFormat    = regexp.MustCompile(`^3\.(0|1)\.(0|[1-9][0-9]{0,2})$`)
	routePattern = regexp.MustCompile(`^/(?:[A-Za-z0-9._~/-]|\{[A-Za-z_][A-Za-z0-9_-]*\})*$`)
	httpMethods  = []string{"delete", "get", "head", "options", "patch", "post", "put", "trace"}
)

func (a *analyzer) readOpenAPI(doc *document) error {
	root := doc.root
	version := oasFormat.FindStringSubmatch(root.get("openapi").text())
	if version == nil {
		return a.gap("CONTRACT_DIALECT_UNSUPPORTED", root.get("openapi").ref(doc))
	}
	if root.get("info").get("version").text() != doc.Version {
		return a.gap("CONTRACT_DOCUMENT_VERSION_MISMATCH", root.get("info").get("version").ref(doc))
	}
	const oas31Dialect = "https://spec.openapis.org/oas/3.1/dialect/base"
	if d := root.get("jsonSchemaDialect"); d != nil && (version[1] != "1" || d.text() != oas31Dialect) {
		return a.gap("CONTRACT_DIALECT_UNSUPPORTED", d.ref(doc))
	}
	doc.dialect = "openapi-3." + version[1]
	for _, key := range []string{"webhooks", "callbacks"} {
		if n := root.get(key); n != nil {
			if err := a.gap("CONTRACT_OPENAPI_SURFACE_UNSUPPORTED", n.ref(doc)); err != nil {
				return err
			}
		}
	}
	components := root.get("components").get("schemas")
	for _, name := range components.keys() {
		if err := a.registerSchema(doc, "/components/schemas/"+pointerPart(name), "bidirectional", components.get(name)); err != nil {
			return err
		}
	}
	paths := root.get("paths")
	if paths == nil || paths.object == nil {
		return a.gap("CONTRACT_PATHS_UNSUPPORTED", paths.ref(doc))
	}
	ids := make(map[string]int)
	for _, item := range paths.object {
		for _, method := range httpMethods {
			if id := item.get(method).get("operationId").text(); id != "" {
				ids[id]++
			}
		}
	}
	for _, route := range paths.keys() {
		item := paths.get(route)
		if !validRoute(route) || item.object == nil || item.get("$ref") != nil {
			if err := a.gap("CONTRACT_PATH_UNSUPPORTED", item.ref(doc)); err != nil {
				return err
			}
			continue
		}
		for _, method := range httpMethods {
			if op := item.get(method); op != nil {
				if err := a.operation(doc, item, op, ids, method, route); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validRoute(route string) bool {
	if len(route) > 512 || !routePattern.MatchString(route) || strings.Contains(route, "//") {
		return false
	}
	for _, segment := range strings.Split(route, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (a *analyzer) operation(doc *document, parent, op *node, ids map[string]int, method, route string) error {
	if op.object == nil || op.get("$ref") != nil {
		return a.gap("CONTRACT_OPERATION_UNSUPPORTED", op.ref(doc))
	}
	server, unique := operationServer(op, parent, doc.root)
	security := op.get("security")
	if security == nil {
		security = doc.root.get("security")
	}
	public := security == nil || security.items != nil && len(security.items) == 0
	id := op.get("operationId").text()
	validID := id == "" && op.get("operationId") == nil || boundedText(id, 256) && ids[id] == 1
	bindable := unique && public && validID && !strings.Contains(route, "{") && !strings.Contains(route, "}")
	if !bindable {
		if err := a.gap("CONTRACT_HTTP_BINDING_CONTEXT_UNRESOLVED", op.ref(doc)); err != nil {
			return err
		}
	}
	entity, err := a.entity(doc, "http", "http_operation", strings.ToUpper(method)+" "+server+" "+route, op.ref(doc))
	if err != nil {
		return err
	}
	a.out.Operations = append(a.out.Operations, Operation{Entity: entity, Method: strings.ToUpper(method), Path: route,
		Server: server, OperationID: id, Bindable: bindable})
	parentPointer := "/paths/" + pointerPart(route)
	pointer := parentPointer + "/" + method
	if err := a.parameters(doc, entity, parentPointer+"/parameters", parent.get("parameters")); err != nil {
		return err
	}
	if err := a.parameters(doc, entity, pointer+"/parameters", op.get("parameters")); err != nil {
		return err
	}
	if body := op.get("requestBody"); body != nil {
		if err := a.media(doc, entity, pointer+"/requestBody", "input", body); err != nil {
			return err
		}
	}
	responses := op.get("responses")
	for _, status := range responses.keys() {
		if err := a.media(doc, entity, pointer+"/responses/"+pointerPart(status), "output", responses.get(status)); err != nil {
			return err
		}
	}
	if responses == nil || responses.object == nil || len(responses.object) == 0 {
		if err := a.gap("CONTRACT_RESPONSES_UNSUPPORTED", responses.ref(doc)); err != nil {
			return err
		}
	}
	if callbacks := op.get("callbacks"); callbacks != nil {
		return a.gap("CONTRACT_OPENAPI_SURFACE_UNSUPPORTED", callbacks.ref(doc))
	}
	return nil
}

func (a *analyzer) parameters(doc *document, op contracts.Entity, pointer string, params *node) error {
	if params == nil {
		return nil
	}
	if params.items == nil {
		return a.gap("CONTRACT_PARAMETERS_UNSUPPORTED", params.ref(doc))
	}
	names := make(map[string]bool)
	for index, param := range params.items {
		place, name, schema := param.get("in").text(), param.get("name").text(), param.get("schema")
		if (place != "query" && place != "path" && place != "header" && place != "cookie") ||
			!boundedText(name, 128) || param.get("$ref") != nil || schema == nil {
			if err := a.gap("CONTRACT_PARAMETERS_UNSUPPORTED", param.ref(doc)); err != nil {
				return err
			}
			continue
		}
		key := place + "/" + name
		if names[key] {
			return a.gap("CONTRACT_PARAMETER_DUPLICATE", param.ref(doc))
		}
		names[key] = true
		if err := a.operationSchema(doc, op, pointer+"/"+strconv.Itoa(index)+"/schema", "input", schema); err != nil {
			return err
		}
	}
	return nil
}

func (a *analyzer) media(doc *document, op contracts.Entity, pointer, direction string, wrapper *node) error {
	if wrapper.object == nil || wrapper.get("$ref") != nil {
		return a.gap("CONTRACT_CONTENT_REF_UNSUPPORTED", wrapper.ref(doc))
	}
	content := wrapper.get("content")
	if content != nil && content.object == nil {
		return a.gap("CONTRACT_MEDIA_UNSUPPORTED", content.ref(doc))
	}
	for _, mime := range content.keys() {
		definition := content.get(mime).get("schema")
		if mime != "application/json" || definition == nil {
			if err := a.gap("CONTRACT_MEDIA_UNSUPPORTED", content.get(mime).ref(doc)); err != nil {
				return err
			}
			continue
		}
		if err := a.operationSchema(doc, op, pointer+"/content/"+pointerPart(mime)+"/schema", direction, definition); err != nil {
			return err
		}
	}
	return nil
}

func (a *analyzer) operationSchema(doc *document, op contracts.Entity, pointer, direction string, data *node) error {
	if doc.schemas[pointer] == nil {
		if err := a.registerSchema(doc, pointer, direction, data); err != nil {
			return err
		}
	}
	if declared := doc.schemas[pointer]; declared != nil {
		return a.link(op.Ref, declared.entity.Ref, "declares_"+direction+"_schema", data.ref(doc))
	}
	return nil
}

func operationServer(choices ...*node) (string, bool) {
	for _, owner := range choices {
		servers := owner.get("servers")
		if servers == nil {
			continue
		}
		if len(servers.items) != 1 {
			return "", false
		}
		server := servers.items[0]
		raw := server.get("url").text()
		if server.get("variables") != nil || !boundedText(raw, 384) ||
			strings.ContainsAny(raw, "{}%\\") || strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
			return "", false
		}
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || u.Opaque != "" {
			return "", false
		}
		if (!u.IsAbs() && u.Host != "") || (u.IsAbs() && (u.Scheme != "https" && u.Scheme != "http" || u.Hostname() == "")) {
			return "", false
		}
		if u.Path != "" && (!validRoute(u.Path) || u.Path != path.Clean(u.Path) && u.Path != path.Clean(u.Path)+"/") ||
			u.Path == "" && !u.IsAbs() {
			return "", false
		}
		return raw, true
	}
	return "/", true
}
