"""Independent standard-library reference for the normative MCE-C14N1 vectors."""
import hashlib
import json
from pathlib import Path


def encode(value, depth=0):
    if depth > 32:
        raise ValueError("depth")
    if value is None:
        return b"n"
    if value is True:
        return b"t"
    if value is False:
        return b"f"
    if type(value) is int:
        if abs(value) > 9007199254740991:
            raise ValueError("integer")
        return b"i" + str(value).encode("ascii") + b";"
    if isinstance(value, str):
        raw = value.encode("utf-8", errors="strict")
        return b"s" + str(len(raw)).encode("ascii") + b":" + raw
    if isinstance(value, list):
        return b"a" + str(len(value)).encode("ascii") + b":" + b"".join(encode(x, depth + 1) for x in value)
    if isinstance(value, dict):
        if len(value) > 4096 or not all(isinstance(k, str) for k in value):
            raise ValueError("keys")
        keys = sorted(value, key=lambda k: k.encode("utf-8", errors="strict"))
        return b"o" + str(len(keys)).encode("ascii") + b":" + b"".join(encode(k, depth + 1) + encode(value[k], depth + 1) for k in keys)
    raise ValueError("unsupported value")


def main():
    text = (Path(__file__).parents[2] / "Maps-Context-Engine-4D_SPEC.md").read_text(encoding="utf-8")
    block = text.split("<!-- BEGIN MCE4D_HASH_VECTORS_V1 -->", 1)[1].split("<!-- END MCE4D_HASH_VECTORS_V1 -->", 1)[0]
    vectors = json.loads(block.split("```json", 1)[1].split("```", 1)[0])["valid"]
    if len(vectors) != 14:
        raise AssertionError("wrong vector count")
    for item in vectors:
        result = b"MCE4D\0C14N1\0" + encode(item["domain"]) + encode(item["value"])
        if result.hex() != item["encoding_hex"] or hashlib.sha256(result).hexdigest() != item["digest"]:
            raise AssertionError(item["case"])
    for invalid in (1.0, 9007199254740992, "\ud800"):
        try:
            encode(invalid)
        except (ValueError, UnicodeEncodeError):
            continue
        raise AssertionError("accepted invalid value")
    print("PASS: 14 normative vectors and invalid values (Python reference)")


if __name__ == "__main__":
    main()
