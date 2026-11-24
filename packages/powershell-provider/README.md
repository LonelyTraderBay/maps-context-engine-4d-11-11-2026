# PowerShell syntax on Windows

The native Windows index and discovery probe inspect `.ps1` declarations
from approved captured bytes. The pins for PowerShell 7.6.9 on Windows amd64
cover its executable, parser assembly, MIT license and third-party notices.
The project includes the probe source and `toolchain-pin.json`. The controller
supplies the pinned distribution.

An owner imports `SyntaxProbe.psm1` and supplies the captured UTF-8 bytes,
portable relative path and expected SHA-256. The result is internal schema
version 1. A `partial` result lists top-level function and filter declarations
with original UTF-8 byte ranges. It does not resolve call targets, infer runtime
module state, load a target file, or create a project profile. The parser
process must use the pinned installation and a trusted owner session.

The input is limited to 64 KiB and bounded delimiter count. Before parsing,
the probe excludes `using`, `#requires`, type references, escaped tokens,
class and DSC declarations. Capture mismatch, parser errors and unsupported
syntax or profiles have separate outcomes and empty declarations. Module
resolution is excluded because PowerShell's symbol resolver can look for
modules while parsing. The probe passes only an in-memory string to the AST
parser. [PowerShell Parser.ParseInput](https://learn.microsoft.com/en-us/dotnet/api/system.management.automation.language.parser.parseinput?view=powershellsdk-7.6.0) and [PowerShell symbol resolver](https://github.com/PowerShell/PowerShell/blob/master/src/System.Management.Automation/engine/parser/SymbolResolver.cs).

Run `probe-test.ps1` by explicit path in the pinned host with `-NoProfile`
and `-NonInteractive`, supplying an owned empty directory with
`-FixtureDirectory`. The test provides authored source as data and checks
that the source command canary remains absent. C079 covers this discovery
profile.

## Native profile

The protected-profile controller stages the approved distribution under
`control/powershell/`. Set the protected ACL on each directory and file with
the same controller and SYSTEM access as the enclosing control directory.
Use a complete distribution so the parser and its built-in modules resolve
from that directory. The worktree policy approves the staged absolute
`pwsh.exe` path and its exact executable SHA-256.

Submit a `PowerShellProfile` in the existing `profile set` request.
`powershellprovider.Profile()` supplies the fields from the executable and
parser pins and computes the digest of the embedded worker program for the
same source revision. Use the returned profile ID for `mce4d index`,
`mce4d query status|locate` and the existing MCP STDIO binding.

Native indexing publishes partial syntax coverage. It captures up to 64
supported `.ps1` files of at most 64 KiB each and 1 MiB total. Source bytes
travel over stdin to the pinned parser in a one-process Windows Job with
`-NoProfile`. Unsupported syntax and parser errors produce diagnostics and
zero declarations for the affected file. MCP can read the published symbol
and its captured source with the existing tools.

## Foreground Watch

Run `mce4d watch --worktree-id <id> --profile-id <id>` with the registered
PowerShell profile. Each poll reconciles the authorized `.ps1` inventory
and captured content. An unchanged capture keeps its generation and leaves
the authenticated WAL receipt and head unchanged. If the capture changes
again during preflight, Watch can reconcile it on the next poll. A changed
capture publishes a new partial generation and removes declarations from
deleted files in the new generation.
CLI queries and existing MCP sessions can read the new generation after
publication. The same source limits, policy and parser pins apply to Watch.
