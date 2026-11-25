# This launcher provisions a disposable local account on the hosted Windows VM.
param([string]$S18Output = '')

$ErrorActionPreference = 'Stop'
if (-not $IsWindows -or $env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') {
    throw 'Run this launcher on a GitHub-hosted Windows runner'
}

$bundle = Join-Path $env:RUNNER_TEMP ($S18Output ? 'mce-s18-live-bin' : 'mce-restore-bin')
$logs = Join-Path $bundle 'logs'
New-Item -ItemType Directory -Path $bundle, $logs | Out-Null
Copy-Item -LiteralPath (Join-Path $env:RUNNER_TEMP 'mce4d.exe') -Destination $bundle
if ($S18Output) {
    Copy-Item -LiteralPath (Join-Path $S18Output 'source') -Destination (Join-Path $bundle 's18-source') -Recurse
    Copy-Item -LiteralPath (Join-Path $S18Output 'live-requests.json') -Destination $bundle
}
go build -trimpath -o (Join-Path $bundle 'state-profile.exe') ./tools/state_profile
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
go test -c -trimpath -o (Join-Path $bundle 'integration.test.exe') ./tests/integration
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$launcher = Join-Path $bundle 'fixture.ps1'
@'
$ErrorActionPreference = 'Stop'
$log = Join-Path $PSScriptRoot 'logs\fixture.log'
try {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'The SQLite fixture requires a standard user'
    }
    $env:USERPROFILE = [Environment]::GetFolderPath('UserProfile')
    $env:LOCALAPPDATA = [Environment]::GetFolderPath('LocalApplicationData')
    if (-not $env:USERPROFILE -or -not $env:LOCALAPPDATA) {
        throw 'The fixture user profile was not loaded'
    }
    $scratch = Join-Path $env:LOCALAPPDATA 'mce-restore-tmp'
    New-Item -ItemType Directory -Path $scratch | Out-Null
    $env:TEMP = $scratch
    $env:TMP = $scratch
    $env:MCE4D_WIN_PACKAGE_FIXTURE = $PSScriptRoot
    if (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'live-requests.json')) {
        $env:MCE4D_WIN_S18_BUNDLE = $PSScriptRoot
    }
    & (Join-Path $PSScriptRoot 'integration.test.exe') '-test.v' '-test.run=^TestWindowsPackagedProfileRoleVerification$' '-test.timeout=4m' *> $log
    exit $LASTEXITCODE
} catch {
    $_ | Out-File -LiteralPath $log -Append
    exit 1
}
'@ | Set-Content -LiteralPath $launcher -Encoding UTF8

$name = 'mceCI-' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$password = ConvertTo-SecureString ([Guid]::NewGuid().ToString('N') + '!aA7') -AsPlainText -Force
$user = New-LocalUser -Name $name -Password $password -AccountNeverExpires
try {
    $users = [Security.Principal.SecurityIdentifier]::new('S-1-5-32-545')
    if (-not (Get-LocalGroupMember -SID $users | Where-Object SID -eq $user.SID)) {
        Add-LocalGroupMember -SID $users -Member $user
    }
    $sid = $user.SID.Value
    icacls.exe $bundle /grant ('*{0}:(OI)(CI)RX' -f $sid) | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant the fixture access to its executables' }
    icacls.exe $logs /grant ('*{0}:(OI)(CI)M' -f $sid) | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant the fixture access to its log directory' }

    $credential = [PSCredential]::new(".\$name", $password)
    $powershell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $arguments = @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'RemoteSigned', '-File', ('"{0}"' -f $launcher))
    $process = Start-Process -FilePath $powershell -ArgumentList $arguments -Credential $credential `
        -LoadUserProfile -UseNewEnvironment -WorkingDirectory $bundle -WindowStyle Hidden -Wait -PassThru
    $result = Get-Content -LiteralPath (Join-Path $logs 'fixture.log')
    $result | Write-Output
    if ($process.ExitCode -ne 0) { throw "Packaged fixture exited $($process.ExitCode)" }
} finally {
    Remove-LocalUser -SID $user.SID
}

if ($S18Output) {
    python -B tools/bench/s18_edit/runner.py live-check --output $S18Output --capture (Join-Path $logs 's18-live.json')
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    $live = Get-Content -Raw -LiteralPath (Join-Path $S18Output 'live-prompt-check.json') | ConvertFrom-Json
    @(
        '### S18 - Windows live prompts'
        ''
        "- Tested commit $env:GITHUB_SHA"
        "- Opened a published Maps index of $($live.maps_index_bytes) bytes (> 4 MiB)."
        '- All 30 live C prompts matched the recorded prompt hashes.'
    ) | Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY
    return
}

foreach ($scope in 'worktree', 'catalog') {
    if (-not ($result -match "packaged $scope restore succeeded on its first invocation after SQLite inspection")) {
        throw "First restore evidence missing for $scope"
    }
}
@(
    '### T035/T037 - Windows first restore regression'
    ''
    "- Tested commit $env:GITHUB_SHA"
    "- Runner $env:RUNNER_OS $env:RUNNER_ARCH"
    "- Toolchain $(go version)"
    '- The isolated worktree and catalog passed every restore step on the first invocation and reported VERIFIED.'
) | Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY
