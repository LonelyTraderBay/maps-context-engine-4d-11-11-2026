"""Compact edit evidence from a caller-authorized, verified Go source view."""

from collections import Counter
import json
import re
import unicodedata


_EDIT_LIMIT = 6000
_SOURCE_LIMIT = 2 << 20
_GO_STRING = re.compile(r'"(?:[^"\\\r\n]|\\.)*"')
_QUOTED = re.compile(r"""['"“‘]([^'"”’\r\n]{10,160})['"”’]""")
_FIELD_REQUEST = re.compile(r"(?:đổi tên trường|doi ten truong)\b.*?\b(?:thành|thanh)\s+([A-Za-z_][A-Za-z0-9_]*)", re.I)
_INTERFACE_REQUEST = re.compile(r"(?:đổi tên|doi ten)\s+(?:chính\s+|chinh\s+)?interface\b.*?\b(?:thành|thanh)\s+([A-Za-z_][A-Za-z0-9_]*)", re.I)
_STRUCT = re.compile(r"(?m)^[ \t]*type[ \t]+[A-Za-z_]\w*[ \t]+struct[ \t]*\{(?P<body>[^{}]*)\}")
_FIELD = re.compile(r"(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]+[^\r\n]+")
_INTERFACE = re.compile(r"(?m)^[ \t]*type[ \t]+([A-Za-z_][A-Za-z0-9_]*)[ \t]+interface[ \t]*\{")
_FUNC_DECL = re.compile(r"^[ \t]*func[ \t]+[A-Za-z_][A-Za-z0-9_]*([ \t]*\()")


def _fold(text):
    accentless = unicodedata.normalize("NFD", text.lower().replace("Đ", "D").replace("đ", "d"))
    return "".join(char for char in accentless if not unicodedata.combining(char))


def _identifier_terms(name):
    words = re.findall(r"[A-Z]+(?=[A-Z][a-z])|[A-Z]?[a-z]+|[A-Z]+|[0-9]+", name)
    return {word.lower().removesuffix("s") if len(word) > 4 else word.lower() for word in words}


def _message_evidence(question, excerpts, sources):
    if "thong bao" not in _fold(question) and "error message" not in question.lower():
        return None
    quoted = _QUOTED.findall(question)
    if len(quoted) != 1:
        return None
    wanted = set(re.findall(r"\w+", quoted[0].lower()))
    if len(wanted) < 4:
        return None
    matches = {}
    for excerpt in excerpts:
        path, start, lines = excerpt
        for number, line in enumerate(lines, start):
            for lit in _GO_STRING.findall(line):
                terms = set(re.findall(r"\w+", lit.lower()))
                similarity = len(terms & wanted) / len(terms | wanted)
                if similarity >= .60 and lit[1:-1] != quoted[0]:
                    matches.setdefault((path, number, lit), (similarity, excerpt))
    ranked = sorted(matches.items(), key=lambda match: -match[1][0])
    if not ranked or len(ranked) > 1 and ranked[0][1][0] - ranked[1][1][0] < .15:
        return None
    (path, _, literal), (_, (_, start, lines)) = ranked[0]
    if sum(body.count(literal) for body in sources.values()) != 1:
        return None
    return {"kind": "message", "path": path, "literal": literal, "occurrences": 1,
            "lines": [[number, line] for number, line in enumerate(lines, start)]}


def _rename_evidence(request, kind, excerpts, sources):
    new_name = request.group(1)
    terms = _identifier_terms(new_name)
    if len(terms) < 2:
        return None
    candidates = set()
    for path, start, lines in excerpts:
        text = "\n".join(lines)
        if kind == "field":
            for struct in _STRUCT.finditer(text):
                offset = text.count("\n", 0, struct.start("body"))
                for field in _FIELD.finditer(struct.group("body")):
                    name = field.group(1)
                    if name != new_name and terms <= _identifier_terms(name):
                        line = start + offset + struct.group("body").count("\n", 0, field.start())
                        candidates.add((path, line, name))
        else:
            for decl in _INTERFACE.finditer(text):
                name = decl.group(1)
                if name != new_name and _identifier_terms(name) <= terms:
                    candidates.add((path, start + text.count("\n", 0, decl.start()), name))
    if len(candidates) != 1:
        return None
    declaration, declaration_line, old_name = candidates.pop()
    boundary = re.compile(rf"\b{re.escape(old_name)}\b")
    files = []
    pattern_count = 0
    for path, body in sources.items():
        expected = sum(1 for _ in boundary.finditer(body))
        if not expected:
            continue
        first_lines = {}
        counts = Counter()
        lines = body.split("\n")
        for number, line in enumerate(lines, 1):
            matches = list(boundary.finditer(line))
            if not matches:
                continue
            line = line.removesuffix("\r")
            # Repeated helpers often differ only by their top-level function
            # names. Omit a varying prefix only if all matches remain visible.
            signature = _FUNC_DECL.match(line)
            text = line[signature.start(1):] if signature and signature.start(1) < matches[0].start() else line
            if len(text) > 450 or len(matches) != 1:
                return None
            first_lines.setdefault(text, number)
            counts[text] += 1
        if sum(counts.values()) != expected or any(body.count(text) != count for text, count in counts.items()):
            return None
        pattern_count += len(counts)
        if pattern_count > 12:
            return None
        groups = []
        for text, count in counts.items():
            line = first_lines[text]
            start = max(1, line - 8)
            end = min(len(lines), line + 4)
            if count > 1:
                end = min(end, line + 1)
            groups.append({
                "line": line, "text": text, "occurrences": count,
                "context_start_line": start,
                "context": "\n".join(part.removesuffix("\r") for part in lines[start - 1:end]),
            })
        files.append({"path": path, "occurrences": expected, "groups": groups})
    return {"kind": kind + "_text_matches", "identifier": old_name, "declaration": declaration,
            "declaration_line": declaration_line, "files": files}


def prepare_go_edit_evidence(question, excerpts, source_view):
    """Return text evidence, with counts over the supplied view only.

    The caller must supply an authorized snapshot or checkout manifest. This
    helper does not access a file system or infer Go bindings from text matches.
    """
    request = _FIELD_REQUEST.search(question)
    kind = "field"
    if not request:
        request = _INTERFACE_REQUEST.search(question)
        kind = "interface"
    if not request and not ("thong bao" in _fold(question) or "error message" in question.lower()):
        return None
    go_files = {path: raw for path, raw in source_view.items() if path.lower().endswith(".go")}
    if len(go_files) > 384 or sum(map(len, go_files.values())) > _SOURCE_LIMIT:
        return None
    try:
        sources = {path: raw.decode("utf-8") for path, raw in sorted(go_files.items())}
    except UnicodeDecodeError:
        return None
    verified = []
    for excerpt in excerpts:
        path, start = excerpt["source_ref"]["path"], excerpt["start_line"]
        if path not in sources or start < 1:
            continue
        physical = sources[path].split("\n")
        lines = excerpt["content"].split("\n")
        if lines[-1] == "":
            lines.pop()
        if lines and lines == physical[start - 1:start - 1 + len(lines)]:
            verified.append((path, start, [line.removesuffix("\r") for line in lines]))
    evidence = _rename_evidence(request, kind, verified, sources) if request else _message_evidence(question, verified, sources)
    if evidence is None or len(json.dumps(evidence, ensure_ascii=False, separators=(",", ":")).encode("utf-8")) > _EDIT_LIMIT:
        return None
    return evidence
