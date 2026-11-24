[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false, $true)
$inputReader = [IO.StreamReader]::new([Console]::OpenStandardInput(), [Console]::OutputEncoding, $false)
try {
    $request = ConvertFrom-Json -InputObject ($inputReader.ReadToEnd()) -AsHashtable -NoEnumerate
    if ($request.Count -gt 64) { exit 3 }
    $rows = [Collections.Generic.List[object]]::new()
    foreach ($file in $request) {
        $answer = & $trustedModule {
            param($item)
            Get-McePowerShellSyntax -Bytes ([Convert]::FromBase64String($item.body)) -Path $item.path -ExpectedHash $item.sha256
        } $file
        $rows.Add($answer)
    }
    [Console]::WriteLine((ConvertTo-Json -InputObject ($rows.ToArray()) -Depth 6 -Compress))
} finally {
    $inputReader.Dispose()
}
