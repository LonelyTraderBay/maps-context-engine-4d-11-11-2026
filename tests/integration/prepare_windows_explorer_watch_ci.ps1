param(
    [Parameter(Mandatory)][string]$Bundle,
    [Parameter(Mandatory)][string]$Browser,
    [Parameter(Mandatory)][string]$BrowserSHA256
)

$ErrorActionPreference = 'Stop'
$repo = Join-Path $PSScriptRoot '../..'
$pin = Get-Content -Raw -LiteralPath (Join-Path $repo 'packages/ts-provider/toolchain-pin.json') | ConvertFrom-Json

function Assert-SHA256([string]$Path, [string]$Expected) {
    if ((Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash -ne $Expected) {
        throw "SHA-256 differs from pin: $([IO.Path]::GetFileName($Path))"
    }
}

# Stage once so the disposable user needs access only to the fixture bundle.
Assert-SHA256 $Browser $BrowserSHA256
Copy-Item -LiteralPath (Split-Path -Parent $Browser) -Destination (Join-Path $Bundle 'browser') -Recurse
Copy-Item -LiteralPath (Join-Path $repo 'tests/acceptance/explorer_windows_watch.mjs') -Destination $Bundle
$modules = Join-Path $Bundle 'node_modules'
New-Item -ItemType Directory -Path $modules | Out-Null
foreach ($package in 'playwright', 'playwright-core') {
    Copy-Item -LiteralPath (Join-Path $repo "node_modules/$package") -Destination $modules -Recurse
}

$material = Join-Path $Bundle 'typescript'
$compiler = Join-Path $material 'compiler'
New-Item -ItemType Directory -Path $compiler | Out-Null
$nodeVersion = $pin.node_windows.version
$nodeDistribution = "node-v$nodeVersion-win-x64"
$nodeArchive = Join-Path $Bundle "$nodeDistribution.zip"
Invoke-WebRequest -Uri "https://nodejs.org/dist/v$nodeVersion/$nodeDistribution.zip" -OutFile $nodeArchive -TimeoutSec 60
Assert-SHA256 $nodeArchive $pin.node_windows.archive_sha256
tar.exe -xf $nodeArchive -C $material --strip-components 1 "$nodeDistribution/node.exe" "$nodeDistribution/LICENSE"
if ($LASTEXITCODE -ne 0) { throw 'Could not extract pinned Windows Node' }
Assert-SHA256 (Join-Path $material 'node.exe') $pin.node_windows.executable_sha256
Assert-SHA256 (Join-Path $material 'LICENSE') $pin.node_windows.license_sha256

$compilerArchive = Join-Path $material "typescript-$($pin.compiler.version).tgz"
Invoke-WebRequest -Uri $pin.compiler.resolved -OutFile $compilerArchive -TimeoutSec 60
Assert-SHA256 $compilerArchive $pin.compiler.archive_sha256
tar.exe -xf $compilerArchive -C $compiler --strip-components 1
if ($LASTEXITCODE -ne 0) { throw 'Could not extract pinned TypeScript' }

$names = [string[]]@(Get-ChildItem -LiteralPath $compiler -File -Recurse -Force | ForEach-Object {
    [IO.Path]::GetRelativePath($compiler, $_.FullName).Replace('\', '/')
})
[Array]::Sort($names, [StringComparer]::Ordinal)
$hashes = [ordered]@{}
foreach ($name in $names) {
    $hashes[$name] = (Get-FileHash -LiteralPath (Join-Path $compiler $name) -Algorithm SHA256).Hash.ToLowerInvariant()
}
$manifest = Join-Path $material 'compiler-files.json'
[IO.File]::WriteAllText($manifest, ($hashes | ConvertTo-Json -Compress) + [char]10)
Assert-SHA256 $manifest $pin.compiler.file_manifest_sha256
