# Python syntax provider

A048 pins CPython 3.14.4 and the installed Ubuntu standard library on Linux amd64. The installation owner supplies the absolute executable and standard library paths. The provider validates their fingerprints, the embedded worker and the closed PythonProfile. Python and its dependencies remain supplied by the operating system. The upstream PSF terms and distribution notices apply to those materials. [CPython 3.14.4 release](https://www.python.org/downloads/release/python-3144/) · [Python license and notices](https://docs.python.org/3.14/license.html)

The worker starts with `-I -S -B` and parses approved UTF-8 captures with `ast.parse`. Files under explicit module roots can contribute module, definition and lexical import-binding facts. Source ranges use UTF-8 byte offsets and preserve captured line endings. Decorators, runtime mutation and computed imports produce coverage gaps. Notebook cells, virtual environments, startup files and project configuration remain inert. [Python command-line flags](https://docs.python.org/3.14/using/cmdline.html) · [Python AST](https://docs.python.org/3.14/library/ast.html)

The baseline returns syntax facts with partial semantic coverage. The installation owner can select `NewTyped` for the separately digested `typed_when_available` profile. Primitive literals in simple assignments carry a type and captured byte range. Annotation expressions, decorator effects, dynamic names, and runtime binding retain gaps. Neither mode loads project imports or configurations.

The typed profile recognizes bounded notebook 4.0–4.5 JSON envelopes. It reports normalized declared format and Python language version when supported. Cells, outputs, arbitrary metadata, and custom kernel execution stay outside the fact stream. Unsupported or incomplete metadata produces a warning with the captured path. [Jupyter notebook format](https://nbformat.readthedocs.io/en/latest/format_description.html)

Native Linux accepts the closed PythonProfile through `profile set` and
indexes it through the authorized worktree/profile binding. The pinned host
installation uses `/usr/bin/python3.14` and `/usr/lib/python3.14`. The
worktree policy must approve the exact interpreter path and digest. Use the
SHA-256 of the build's `worker.py` as the provider digest. The CLI and MCP
read the same published generation. Native capture covers Python source,
with setup, startup, environment and notebook files excluded. Both
`syntax` and `typed_when_available` retain the provider's coverage limits.

On WSL2, run `mce4d watch --worktree-id <id> --profile-id <id>` with the
registered profile. Watch reconciles approved `.py` captures in the selected
module roots. Additions, edits and deletions publish a generation that the
CLI and an existing MCP session can read. An unchanged capture keeps the
generation and WAL head. If source drifts while a generation is being built,
Watch retries on the next poll after cleanup and authority verification.

On Windows amd64, the controller stages the pinned CPython 3.14.4 embedded
distribution in `control/python/` under the protected profile. The files use
the controller and SYSTEM ACL. `toolchain-windows-pin.json` binds the archive
and extracted files. `WindowsProfile` supplies the Windows interpreter,
standard library ZIP and worker digests for `profile set`. The worktree
policy approves the exact staged `python.exe` and its digest. The parser
uses `-I -S -B`, the pinned `._pth`, a one-process Windows Job and captured
`.py` files. Both `index` and `watch` use the same worktree/profile IDs as
CLI and MCP queries. [CPython 3.14.4](https://www.python.org/downloads/release/python-3144/)
