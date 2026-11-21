"""Build the pinned Windows AppContainer compatibility variant offline."""

from argparse import ArgumentParser
from base64 import b64encode
from hashlib import sha256
import json
import os
from pathlib import Path
import subprocess
from zipfile import ZipFile, ZipInfo


MODULE = "golang.org/x/tools/gopls"
BASE = "v0.24.0"
VERSION = "v0.24.0-mce4d.windows.1"
BASE_SUM = "h1:nPO/LffYa4sP62Ueuema5PPnZPGaZjGl+vZGvbTek4g="
VERSION_SUM = "h1:Qr4n2Q1AmM9EzszvKdbwTd7vdppENxjTi+R2Smo/ZYc="
PATCH_SHA256 = "753ddf0c8e22e5181ff5c8da5d0f27379159aadf166abdc2237ef29bc964c3b5"


def build(go: Path, preflight: Path, output: Path) -> None:
    approved_proxy = preflight / "modcache/cache/download"
    approved_source = approved_proxy / MODULE / "@v"
    replacement = Path(__file__).with_name("os_windows.go.txt").read_bytes().replace(b"\r\n", b"\n").rstrip(b"\n")
    if sha256(replacement).hexdigest() != PATCH_SHA256:
        raise ValueError("reviewed Windows source digest mismatch")

    base_zip = approved_source / (BASE + ".zip")
    if base_zip.stat().st_size > 16 << 20:
        raise ValueError("approved module archive exceeds the input limit")
    with ZipFile(base_zip) as archive:
        entries = [entry for entry in archive.infolist() if not entry.is_dir()]
        if len(entries) > 8192 or sum(entry.file_size for entry in entries) > 64 << 20:
            raise ValueError("approved gopls source exceeds the input limit")
        files = {entry.filename: archive.read(entry) for entry in entries}
    prefix = f"{MODULE}@{BASE}/"
    if any(not name.startswith(prefix) for name in files):
        raise ValueError("unexpected module archive path")
    checksums = "".join(f"{sha256(files[name]).hexdigest()}  {name}\n" for name in sorted(files))
    module_sum = "h1:" + b64encode(sha256(checksums.encode()).digest()).decode()
    if module_sum != BASE_SUM:
        raise ValueError("original module checksum mismatch")
    original = prefix + "internal/cache/os_windows.go"
    if original not in files:
        raise ValueError("expected Windows source missing")
    files[original] = replacement

    proxy = output / "proxy"
    package = proxy / MODULE / "@v"
    package.mkdir(parents=True)
    with ZipFile(package / (VERSION + ".zip"), "w") as archive:
        for name in sorted(files):
            archive.writestr(ZipInfo(f"{MODULE}@{VERSION}/" + name.removeprefix(prefix)), files[name])
    (package / (VERSION + ".info")).write_text(json.dumps({"Version": VERSION, "Time": "2026-11-20T00:00:00Z"}))
    (package / (VERSION + ".mod")).write_bytes((approved_source / (BASE + ".mod")).read_bytes())

    directory = preflight / "build"
    mod = output / "reviewed.mod"
    locked = (directory / "go.mod").read_text()
    if locked.count(f"{MODULE} {BASE}") != 1:
        raise ValueError("expected pinned gopls requirement missing")
    mod.write_text(locked.replace(f"{MODULE} {BASE}", f"{MODULE} {VERSION}"))
    mod.with_suffix(".sum").write_bytes((directory / "go.sum").read_bytes())
    env = dict(os.environ, GOPROXY=f"{proxy.as_uri()},{approved_proxy.as_uri()}", GOSUMDB="off", GOTOOLCHAIN="local",
               GOOS="windows", GOARCH="amd64", GOAMD64="v1", CGO_ENABLED="0", GOTELEMETRY="off", GOMODCACHE=str(output / "modcache"),
               GOWORK="off", GOENV="off", GOFLAGS="-mod=readonly")
    verified = subprocess.check_output([go, "mod", "download", "-modfile", mod, "-json", f"{MODULE}@{VERSION}"],
                                       cwd=directory, env=env, text=True)
    if json.loads(verified).get("Sum") != VERSION_SUM:
        raise ValueError("Windows compatibility module checksum mismatch")
    binary = output / "gopls.exe"
    subprocess.run([go, "build", "-trimpath", "-buildvcs=false", "-modfile", mod, "-o", binary, MODULE],
                   cwd=directory, env=env, check=True)
    subprocess.run([go, "version", "-m", binary], cwd=directory, env=env, check=True)
    print(sha256(binary.read_bytes()).hexdigest(), flush=True)


if __name__ == "__main__":
    parser = ArgumentParser(description=__doc__)
    parser.add_argument("--go", type=Path, required=True)
    parser.add_argument("--preflight", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(mode=0o700)
    build(args.go.resolve(strict=True), args.preflight.resolve(strict=True), output)
