Set-StrictMode -Version Latest

$script:Pin = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'toolchain-pin.json') -Raw | ConvertFrom-Json -AsHashtable
$script:Utf8 = [Text.UTF8Encoding]::new($false, $true)

function Test-ParserInstallation {
    if (-not $IsWindows -or [Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString() -ne 'X64' -or
        $PSVersionTable.PSVersion.ToString() -cne $script:Pin.powershell_version) {
        return $false
    }
    $processPath = [Diagnostics.Process]::GetCurrentProcess().MainModule.FileName
    $assemblyPath = [System.Management.Automation.Language.Parser].Assembly.Location
    if ($processPath -ne (Join-Path $PSHOME 'pwsh.exe') -or
        $assemblyPath -ne (Join-Path $PSHOME 'System.Management.Automation.dll')) {
        return $false
    }
    foreach ($entry in $script:Pin.files.GetEnumerator()) {
        $file = Join-Path $PSHOME $entry.Key
        if (-not (Test-Path -LiteralPath $file -PathType Leaf) -or
            (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant() -cne $entry.Value) {
            return $false
        }
    }
    return $true
}

function Get-McePowerShellSyntax {
    <#
    .SYNOPSIS
    Describes supported declarations in UTF-8 bytes already obtained by an authorized capture owner.
    #>
    param(
        [Parameter(Mandatory)][AllowEmptyCollection()][byte[]] $Bytes,
        [Parameter(Mandatory)][string] $Path,
        [Parameter(Mandatory)][string] $ExpectedHash,
        [string] $Mode = 'syntax'
    )

    $result = [ordered]@{
        schema_version = 1
        language = 'powershell'
        parser_profile = 'powershell-syntax-discovery-v1'
        status = 'unavailable'
        reason = 'INSTALLATION_UNVERIFIED'
        path = $null
        content_hash = $null
        declarations = @()
    }
    if (-not (Test-ParserInstallation)) {
        return $result
    }
    if ($Mode -cne 'syntax') {
        $result.status = 'unsupported'
        $result.reason = 'PROFILE_UNSUPPORTED'
        return $result
    }
    if ($Bytes.Length -gt 65536 -or $Path.Length -gt 256 -or
        $Path -cnotmatch '^(?:[A-Za-z0-9_-][A-Za-z0-9_.-]*/)*[A-Za-z0-9_-][A-Za-z0-9_.-]*\.ps1$' -or
        $ExpectedHash -cnotmatch '^[a-f0-9]{64}$') {
        $result.status = 'rejected'
        $result.reason = 'CAPTURE_INVALID'
        return $result
    }
    $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($Bytes)).ToLowerInvariant()
    if ($hash -cne $ExpectedHash) {
        $result.status = 'rejected'
        $result.reason = 'CAPTURE_INVALID'
        return $result
    }
    try {
        $source = $script:Utf8.GetString($Bytes)
    } catch [Text.DecoderFallbackException] {
        $result.status = 'rejected'
        $result.reason = 'CAPTURE_INVALID'
        return $result
    }
    $secret = '(?i)\b(?:api[_-]?key|access[_-]?token|client[_-]?secret|private[_-]?key|password)\b\s*[:=]\s*["'']?[A-Za-z0-9_+/=-]{16,}'
    if ($source.Contains([char]0) -or $source -match $secret -or $source -match '-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----') {
        $result.status = 'rejected'
        $result.reason = 'CONTENT_DENIED'
        return $result
    }
    $result.path = $Path
    $result.content_hash = $hash

    # Reject all forms of type references and module discovery before calling
    # the parser. This small profile also excludes escaped tokens and DSC.
    if ($source -match '[\x60\[\]]|#\s*requires|\b(?:using|class|enum|configuration|workflow)\b' -or
        [regex]::Matches($source, '[(){}]').Count -gt 256) {
        $result.status = 'unsupported'
        $result.reason = 'SYNTAX_OUTSIDE_PROFILE'
        return $result
    }

    $tokens = $null
    $errors = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$errors)
    if ($errors.Length -ne 0) {
        $result.status = 'parse_error'
        $result.reason = 'INVALID_SYNTAX'
        return $result
    }
    if ($ast.BeginBlock -or $ast.ProcessBlock -or $ast.DynamicParamBlock -or $ast.CleanBlock) {
        $result.status = 'unsupported'
        $result.reason = 'SYNTAX_OUTSIDE_PROFILE'
        return $result
    }
    $declarations = [Collections.Generic.List[object]]::new()
    if ($null -ne $ast.EndBlock) {
        foreach ($statement in $ast.EndBlock.Statements) {
            if ($statement -isnot [System.Management.Automation.Language.FunctionDefinitionAst]) {
                continue
            }
            if ($statement.Name -cnotmatch '^[A-Za-z_][A-Za-z0-9_-]{0,95}$') {
                $result.status = 'unsupported'
                $result.reason = 'SYNTAX_OUTSIDE_PROFILE'
                return $result
            }
            $declarations.Add([ordered]@{
                name = $statement.Name
                kind = if ($statement.IsFilter) { 'filter' } else { 'function' }
                start_byte = $script:Utf8.GetByteCount($source.Substring(0, $statement.Extent.StartOffset))
                end_byte = $script:Utf8.GetByteCount($source.Substring(0, $statement.Extent.EndOffset))
            })
        }
    }
    $result.status = 'partial'
    $result.reason = 'SYNTAX_DECLARATIONS_ONLY'
    $result.declarations = $declarations.ToArray()
    return $result
}

Export-ModuleMember -Function Get-McePowerShellSyntax
