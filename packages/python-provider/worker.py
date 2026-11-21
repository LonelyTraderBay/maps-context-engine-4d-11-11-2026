"""MCE4D installation-owned parse-only CPython worker."""
import os
import sys

stdlib = os.environ["MCE4D_PYTHON_STDLIB"]
zip_name = os.path.basename(stdlib).replace(".", "") + ".zip"
allowed_path = [os.path.join(os.path.dirname(stdlib), zip_name), stdlib, os.path.join(stdlib, "lib-dynload")]
if sys.path != allowed_path or not sys.flags.isolated or not sys.flags.no_site or not sys.dont_write_bytecode:
    raise SystemExit(1)
sys.path[:] = allowed_path[1:]

import ast
import hashlib
import json
import re
import resource

for module in tuple(sys.modules.values()):
    name = getattr(module, "__file__", None)
    if name and module is not sys.modules["__main__"] and not os.path.realpath(name).startswith(stdlib + "/"):
        raise SystemExit(1)
resource.setrlimit(resource.RLIMIT_AS, (512 << 20, 512 << 20))

MAX_FILE = 1 << 20
MAX_NODES = 100_000
MAX_FILES = 2048
MAX_MANIFEST = 16 << 20
MAX_STREAM = 32 << 20
MAX_NOTEBOOK = 512 << 10
ENV_DIRS = {".venv", "venv", "__pycache__", "site-packages"}
SETUP_FILES = {"sitecustomize.py", "usercustomize.py", "pyvenv.cfg", "pyproject.toml", "setup.py", "setup.cfg"}
LITERAL_TYPES = {bool: "bool", int: "int", float: "float", complex: "complex",
                 str: "str", bytes: "bytes", type(None): "None"}
GAP_REASONS = {
    "PYTHON_SYNTAX_ONLY": "Only captured Python syntax and lexical declarations are supported.",
    "PYTHON_PARSE_PARTIAL": "Some captured Python source could not be parsed within this profile.",
    "PYTHON_DYNAMIC_IMPORT": "At least one runtime import target is unresolved.",
    "PYTHON_DECORATOR": "Decorator effects and substituted callables are unresolved.",
    "PYTHON_RUNTIME_MUTATION": "Runtime attribute or namespace mutation is unresolved.",
    "PYTHON_NOTEBOOK_IGNORED": "Notebook source cells and outputs were not analyzed or executed.",
    "PYTHON_ENV_IGNORED": "Project environment and startup metadata were not loaded.",
    "PYTHON_OUTSIDE_MODULE_ROOT": "Some Python files were outside the declared module roots.",
    "PYTHON_PARSE_LIMIT": "The bounded parser stopped before inspecting every file or declaration.",
    "PYTHON_TYPE_COVERAGE": "Type enrichment is limited to primitive literal expressions in simple assignments.",
    "PYTHON_ANNOTATION_UNRESOLVED": "Some annotation expressions have no verified binding in this profile.",
    "PYTHON_NOTEBOOK_METADATA_UNKNOWN": "Some notebook metadata was unavailable or outside the supported envelope.",
}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def stable_id(kind, *parts):
    data = json.dumps(parts, ensure_ascii=False, separators=(",", ":")).encode()
    return kind + "-" + digest(b"MCE4D-PYTHON1\0" + data)


def read_start():
    source = sys.stdin.buffer
    header = source.readline(128)
    match = re.fullmatch(rb"Content-Length: ([0-9]{1,8})\r\n", header)
    if not match or source.read(2) != b"\r\n":
        raise ValueError()
    count = int(match[1])
    if not 1 <= count <= 65536:
        raise ValueError()
    raw = source.read(count)
    if len(raw) != count:
        raise ValueError()
    return json.loads(raw)


def read_capture(path, budget):
    with open(path, "rb") as source:
        raw = source.read(budget + 1)
    if len(raw) > budget:
        raise ValueError()
    return raw


def module_root(path, roots):
    candidates = [root for root in roots if root == "." or path.startswith(root + "/")]
    return max(candidates, key=len, default=None)


def module_name(path, root):
    name = path if root == "." else path[len(root) + 1:]
    parts = name[:-3].split("/")
    if parts[-1] == "__init__" and len(parts) > 1:
        parts.pop()
    dotted = ".".join(parts)
    if len(dotted) > 2048 or not all(part.isidentifier() for part in parts):
        dotted = stable_id("module", name)
    return dotted


def line_offsets(raw):
    return [0] + [m.end() for m in re.finditer(rb"\r\n|\r|\n", raw)]


def definitions(tree, raw, path, package_key, module, gaps, enrich):
    lines = line_offsets(raw)
    symbols = []
    pending = [(tree, module, stable_id("scope", path), False)]
    count = 0

    def add(node, kind, name, qualified, scope, signature, exported):
        start = lines[node.lineno - 1] + node.col_offset
        end = lines[node.end_lineno - 1] + node.end_col_offset
        if not 0 <= start <= end <= len(raw):
            raise ValueError()
        if len(name) > 512 or len(qualified) > 2048:
            gaps.add("PYTHON_PARSE_LIMIT")
            return
        symbols.append({
            "symbol_id": stable_id("symbol", path, scope, kind, name, start),
            "path": path, "package_key": package_key, "kind": kind,
            "name": name, "qualified_name": qualified, "scope_key": scope,
            "range": {"start_byte": start, "end_byte": end},
            "signature": signature, "exported": exported,
        })

    while pending:
        count += 1
        if count > MAX_NODES or len(symbols) >= 4096:
            gaps.add("PYTHON_PARSE_LIMIT")
            break
        node, qualified, lexical, is_class = pending.pop()
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            kind = "type" if isinstance(node, ast.ClassDef) else "method" if is_class else "function"
            child = qualified + "." + node.name
            flavor = "class" if kind == "type" else "async_def" if isinstance(node, ast.AsyncFunctionDef) else "def"
            add(node, kind, node.name, child, lexical, flavor, qualified == module and not node.name.startswith("_"))
            if node.decorator_list:
                gaps.add("PYTHON_DECORATOR")
            qualified, lexical = child, stable_id("scope", path, lexical, node.name, node.lineno, node.col_offset)
            is_class = kind == "type"
        elif isinstance(node, (ast.Import, ast.ImportFrom)):
            for alias in node.names:
                name = alias.asname or alias.name.split(".", 1)[0]
                add(alias, "variable", name, qualified + "." + name, lexical, "import_binding", False)
        elif isinstance(node, ast.Call):
            leaf = getattr(node.func, "id", getattr(node.func, "attr", ""))
            if leaf in {"__import__", "import_module"}:
                gaps.add("PYTHON_DYNAMIC_IMPORT")
            if leaf in {"setattr", "delattr", "globals", "exec", "eval"}:
                gaps.add("PYTHON_RUNTIME_MUTATION")
        elif isinstance(node, ast.Attribute) and isinstance(node.ctx, (ast.Store, ast.Del)):
            gaps.add("PYTHON_RUNTIME_MUTATION")
        elif enrich and isinstance(node, (ast.Assign, ast.AnnAssign)):
            target = node.target if isinstance(node, ast.AnnAssign) else node.targets[0] if len(node.targets) == 1 else None
            if isinstance(node, ast.AnnAssign):
                gaps.add("PYTHON_ANNOTATION_UNRESOLVED")
            if isinstance(target, ast.Name) and isinstance(node.value, ast.Constant):
                literal = LITERAL_TYPES.get(type(node.value.value))
                if literal:
                    name = target.id
                    add(node, "variable", name, qualified + "." + name, lexical,
                        "literal_expression_type:" + literal, qualified == module and not name.startswith("_"))
        if enrich and (isinstance(node, ast.arg) and node.annotation is not None
                       or isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.returns is not None
                       or isinstance(node, ast.TypeAlias)):
            gaps.add("PYTHON_ANNOTATION_UNRESOLVED")
        pending.extend((child, qualified, lexical, is_class) for child in reversed(list(ast.iter_child_nodes(node))))
    return symbols


def unique_fields(pairs):
    fields = {}
    for key, value in pairs:
        if key in fields:
            raise ValueError()
        fields[key] = value
    return fields


def invalid_constant(_):
    raise ValueError()


def notebook_metadata(raw):
    try:
        doc = json.loads(raw.decode("utf-8"), object_pairs_hook=unique_fields, parse_constant=invalid_constant)
    except (ValueError, RecursionError):
        return None
    if not isinstance(doc, dict):
        return None
    major, minor = doc.get("nbformat"), doc.get("nbformat_minor")
    md = doc.get("metadata")
    if type(major) is not int or major != 4 or type(minor) is not int or not 0 <= minor <= 5:
        return None
    if not isinstance(md, dict) or not isinstance(doc.get("cells"), list):
        return None
    language = md.get("language_info")
    kernel = md.get("kernelspec")
    if not isinstance(language, dict) or language.get("name") != "python":
        return None
    if kernel is not None and (not isinstance(kernel, dict) or kernel.get("name") not in ("python", "python3")):
        return None
    version = language.get("version")
    if version is not None and (not isinstance(version, str) or not re.fullmatch(r"3\.[0-9]{1,2}\.[0-9]{1,3}", version)):
        return None
    return f"Declared Python notebook metadata: format 4.{minor}; language version {version or 'unknown'}."


def diagnostic(path, package, code, severity, message, scope):
    return {"diagnostic_id": stable_id("diagnostic", path, code), "path": path, "package_key": package,
            "code": code, "severity": severity, "message": message, "scope_description": scope}


def analyze(start):
    profile = start["profile"]
    version = ".".join(map(str, sys.version_info[:3]))
    if (sys.implementation.name != "cpython" or profile["python_version"] != version
            or profile["analysis_mode"] not in ("syntax", "typed_when_available") or profile["allow_project_imports"]):
        raise ValueError()
    root = start["snapshot_root"]
    if os.path.realpath(".") != root or os.path.dirname(start["manifest_path"]) != root:
        raise ValueError()
    raw = read_capture(start["manifest_path"], MAX_MANIFEST)
    if digest(raw) != start["manifest_digest"]:
        raise ValueError()
    manifest = json.loads(raw)
    if manifest["profile_id"] != start["profile_id"] or manifest["provider_digest"] != start["provider_digest"]:
        raise ValueError()
    gap_codes = {"PYTHON_SYNTAX_ONLY"}
    enrich = profile["analysis_mode"] == "typed_when_available"
    typed_count = 0
    notebook_count = 0
    if enrich:
        gap_codes.remove("PYTHON_SYNTAX_ONLY")
        gap_codes.add("PYTHON_TYPE_COVERAGE")
    sequence = 1
    written = 0
    limit = start["max_frame_bytes"]

    def emit(kind, values):
        nonlocal sequence, written
        message = {"provider_protocol": 1, "type": kind, "job_id": start["job_id"], "sequence": sequence, **values}
        payload = json.dumps(message, ensure_ascii=False, separators=(",", ":")).encode()
        if len(payload) > limit or written + len(payload) > MAX_STREAM:
            raise ValueError()
        sys.stdout.buffer.write(f"Content-Length: {len(payload)}\r\n\r\n".encode() + payload)
        sys.stdout.buffer.flush()
        written += len(payload)
        sequence += 1

    count = 0
    for entry in sorted(manifest["files"], key=lambda item: item["path"]):
        if entry["status"] != "captured":
            continue
        path = entry["path"]
        notebook = path.endswith(".ipynb")
        if notebook:
            gap_codes.add("PYTHON_NOTEBOOK_IGNORED")
        if ENV_DIRS.intersection(path.split("/")) or path.endswith(".pth") or os.path.basename(path) in SETUP_FILES:
            gap_codes.add("PYTHON_ENV_IGNORED")
            continue
        if not (path.endswith(".py") or notebook and enrich):
            continue
        module_base = module_root(path, profile["module_roots"])
        if module_base is None:
            gap_codes.add("PYTHON_OUTSIDE_MODULE_ROOT")
            continue
        count += 1
        if count > MAX_FILES:
            gap_codes.add("PYTHON_PARSE_LIMIT")
            break
        budget = MAX_NOTEBOOK if notebook else MAX_FILE
        if entry["byte_length"] > budget:
            gap_codes.add("PYTHON_PARSE_LIMIT")
            continue
        raw = read_capture(path, budget)
        if len(raw) != entry["byte_length"] or digest(raw) != entry["content_hash"]:
            raise ValueError()
        if notebook:
            description = notebook_metadata(raw)
            if description is None:
                gap_codes.add("PYTHON_NOTEBOOK_METADATA_UNKNOWN")
                item = diagnostic(path, None, "PYTHON_NOTEBOOK_METADATA_UNKNOWN", "warning",
                                  "Notebook declared metadata is incomplete or unsupported.", "Notebook envelope only")
            else:
                notebook_count += 1
                item = diagnostic(path, None, "PYTHON_NOTEBOOK_DECLARED", "info", description, "Notebook envelope only")
            emit("analyze_batch", {"packages": [], "symbols": [], "relations": [], "diagnostics": [item]})
            continue
        package = stable_id("package", path, module_base)
        try:
            tree = ast.parse(raw.decode("utf-8"), filename="<captured>", mode="exec")
        except (UnicodeError, SyntaxError, RecursionError):
            gap_codes.add("PYTHON_PARSE_PARTIAL")
            if enrich:
                item = diagnostic(path, None, "PYTHON_PARSE_PARTIAL", "warning",
                                  "Unable to parse captured Python source within the profile.", "Captured UTF-8 source")
                emit("analyze_batch", {"packages": [], "symbols": [], "relations": [], "diagnostics": [item]})
            continue
        module = module_name(path, module_base)
        fields = definitions(tree, raw, path, package, module, gap_codes, enrich)
        typed_count += sum(item["signature"].startswith("literal_expression_type:") for item in fields)
        package_fact = {"package_key": package, "import_path": module, "module_key": stable_id("root", module_base),
                        "variant": "production", "input_digest": manifest["snapshot_digest"], "semantic_status": "partial"}
        emit("analyze_batch", {"packages": [package_fact], "symbols": [], "relations": [], "diagnostics": []})
        for offset in range(0, len(fields), 8):
            emit("analyze_batch", {"packages": [], "symbols": fields[offset:offset + 8], "relations": [], "diagnostics": []})
    capabilities = [{"name": "python_syntax", "provider_id": start["provider_id"],
                     "provider_version": version, "status": "partial"}]
    if enrich:
        for name, count in (("python_primitive_literal_types", typed_count),
                            ("python_nbformat4_declared_metadata", notebook_count)):
            capabilities.append({"name": name, "provider_id": start["provider_id"], "provider_version": version,
                                 "status": "partial" if count else "unknown"})
    emit("analyze_done", {"status": "partial", "input_digest": manifest["snapshot_digest"],
         "capabilities": capabilities,
         "gaps": [{"code": code, "reason": GAP_REASONS[code], "count": None, "source_refs": []}
                  for code in sorted(gap_codes)], "error": None})


try:
    analyze(read_start())
except (ValueError, TypeError, KeyError, OSError, MemoryError, IndexError):
    raise SystemExit(1) from None
