param([Parameter(Mandatory)][string] $FixtureDirectory)

$ErrorActionPreference = 'Stop'
Import-Module -Name (Join-Path $PSScriptRoot 'SyntaxProbe.psm1') -Force
$utf8 = [Text.UTF8Encoding]::new($false, $true)

function Invoke-Probe([string] $Text, [string] $Mode = 'syntax') {
    $bytes = $utf8.GetBytes($Text)
    $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
    Get-McePowerShellSyntax -Bytes $bytes -Path 'scripts/source.ps1' -ExpectedHash $hash -Mode $Mode
}

$root = (Resolve-Path -LiteralPath $FixtureDirectory).Path
if ((Get-ChildItem -Force -LiteralPath $root | Measure-Object).Count -ne 0) {
    throw 'Probe needs an empty owned fixture directory'
}
Push-Location -LiteralPath $root
try {
    $prefix = "# 🗺 source café`r`n"
    $declaration = 'function Read-Map { "function Hidden {}" }'
    $second = 'filter Get-Map { $_ }'
    $source = $prefix + $declaration + "`r`n" + $second + "`r`n" + 'New-Item -Path ./repo-executed -ItemType File'
    $found = Invoke-Probe $source
    if ($found.status -cne 'partial' -or $found.reason -cne 'SYNTAX_DECLARATIONS_ONLY' -or
        $found.declarations.Count -ne 2 -or $found.declarations[0].name -cne 'Read-Map' -or
        $found.declarations[1].name -cne 'Get-Map' -or $found.declarations[1].kind -cne 'filter' -or
        $found.declarations[0].start_byte -ne $utf8.GetByteCount($prefix) -or
        $found.declarations[0].end_byte -ne $utf8.GetByteCount($prefix + $declaration) -or
        (Test-Path -LiteralPath (Join-Path $root 'repo-executed'))) {
        throw "Declaration or no-execution boundary failed ($($found.reason))"
    }
    $cases = @(
        @{ name = 'syntax_error'; script = 'function Incomplete {'; mode = 'syntax'; expected = 'parse_error' }
        @{ name = 'typed_profile'; script = 'function Demo {}'; mode = 'typed_when_available'; expected = 'unsupported' }
        @{ name = 'load_directive'; script = 'using module ./owner-fixture.psm1'; mode = 'syntax'; expected = 'unsupported' }
        @{ name = 'type_reference'; script = 'function Demo { [Alias] }'; mode = 'syntax'; expected = 'unsupported' }
        @{ name = 'module_requirements'; script = '#Requires -Modules OwnerFixture'; mode = 'syntax'; expected = 'unsupported' }
        @{ name = 'dynamic_dsc'; script = 'configuration Demo {}'; mode = 'syntax'; expected = 'unsupported' }
        @{ name = 'sensitive_content'; script = ('$password = ' + ('z' * 32)); mode = 'syntax'; expected = 'rejected' }
        @{ name = 'oversized_capture'; script = ('x' * 65537); mode = 'syntax'; expected = 'rejected' }
    )
    foreach ($scenario in $cases) {
        $probe = Invoke-Probe $scenario.script $scenario.mode
        if ($probe.status -cne $scenario.expected -or $probe.declarations.Count -ne 0) {
            throw "Unexpected discovery metadata in $($scenario.name)"
        }
    }
    $wrongHash = Get-McePowerShellSyntax -Bytes ($utf8.GetBytes($source)) -Path 'scripts/source.ps1' -ExpectedHash ('0' * 64)
    if ($wrongHash.status -cne 'rejected' -or $wrongHash.path -ne $null -or $wrongHash.declarations.Count -ne 0) {
        throw 'Mismatching capture accepted'
    }
    [ordered]@{
        check = 'T079_C079'
        status = 'PASS'
        windows_arch = [Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString()
        powershell = $PSVersionTable.PSVersion.ToString()
        declarations = $found.declarations
        source_sha256 = $found.content_hash
        negative_cases = @($cases.name) + @('wrong_capture_hash')
        canary_exists = Test-Path -LiteralPath (Join-Path $root 'repo-executed')
        enabled_by_default = $false
    } | ConvertTo-Json -Depth 5 -Compress
} finally {
    Pop-Location
}
