# PowerShell syntax discovery

The Windows ownership and protected-profile workflow uses PowerShell as a host
language. This opt-in probe inspects `.ps1` declarations from approved captured
bytes in that environment. It uses the local PowerShell 7.6.9 Windows amd64
installation whose executable, parser assembly, MIT license and third-party
notices are pinned in `toolchain-pin.json`. These binaries and libraries are
external materials. The project includes the probe source and pin.

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
profile only. Core CLI/MCP indexing does not register this language addon.
