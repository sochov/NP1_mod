param([string]$Interface = 'Wi-Fi', [ValidateRange(0,240)][int]$HoldSeconds = 0, [string]$ReadyFile = '', [string]$MonitorPath = '', [string]$AccessFile = '')
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$trialTmpRoot = [System.IO.Path]::GetFullPath("$repo/tmp/")
foreach ($trialOutput in @($ReadyFile,$MonitorPath)) {
    if ($trialOutput -and -not [System.IO.Path]::GetFullPath($trialOutput).StartsWith($trialTmpRoot,[System.StringComparison]::OrdinalIgnoreCase)) { throw 'Private trial outputs must stay beneath repository tmp.' }
}
if (-not $AccessFile) { $AccessFile = "$repo/tmp/ap-client-access.json" }
$access = Get-Content -LiteralPath $AccessFile -Raw | ConvertFrom-Json
$adapter = Get-NetAdapter -Name $Interface
if ($adapter.Status -ne 'Disconnected') { throw 'Use a disconnected Wi-Fi adapter; do not replace an existing connection.' }
$profile = 'ncc-test-' + [guid]::NewGuid().ToString('N')
$ssid = [System.Security.SecurityElement]::Escape($access.ssid)
$key = [System.Security.SecurityElement]::Escape($access.password)
$webSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$xmlPath = "$repo/tmp/$profile.xml"
$xml = @"
<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1"><name>$profile</name><SSIDConfig><SSID><name>$ssid</name></SSID></SSIDConfig><connectionType>ESS</connectionType><connectionMode>manual</connectionMode><MSM><security><authEncryption><authentication>WPA2PSK</authentication><encryption>AES</encryption><useOneX>false</useOneX></authEncryption><sharedKey><keyType>passPhrase</keyType><protected>false</protected><keyMaterial>$key</keyMaterial></sharedKey></security></MSM></WLANProfile>
"@
[System.IO.File]::WriteAllText($xmlPath, $xml, [System.Text.UTF8Encoding]::new($false))
try {
    & netsh wlan add profile filename=$xmlPath interface=$Interface user=current | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Cannot add temporary Wi-Fi profile.' }
    & netsh wlan connect name=$profile interface=$Interface | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Cannot request AP connection.' }
    $deadline = (Get-Date).AddSeconds(35)
    $address = $null
    do {
        Start-Sleep -Milliseconds 500
        $address = Get-NetIPAddress -InterfaceAlias $Interface -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object { $_.IPAddress -like '192.168.226.*' }
    } while (-not $address -and (Get-Date) -lt $deadline)
    if (-not $address) { throw 'AP DHCP lease was not received.' }
    $response = Invoke-WebRequest -WebSession $webSession -UseBasicParsing -Uri 'http://192.168.226.1/' -TimeoutSec 10
    if ($response.StatusCode -ne 200 -or $response.Content -notmatch '<div id="app">') { throw 'AP Web UI was not confirmed.' }
    $stationDeadline = (Get-Date).AddSeconds(7)
    do {
        $status = Invoke-RestMethod -WebSession $webSession -Uri 'http://192.168.226.1/api/v1/network/hotspot' -TimeoutSec 10
        if ($status.data.clientCount -ge 1) { break }
        Start-Sleep -Milliseconds 500
    } while ((Get-Date) -lt $stationDeadline)
    if ($status.data.enabled -ne $true -or $status.data.dhcpReady -ne $true -or $status.data.clientCount -lt 1) { throw 'AP station state was not confirmed.' }
    if ($ReadyFile) { [System.IO.File]::WriteAllText([System.IO.Path]::GetFullPath($ReadyFile),'ready') }
    $monitorDeadline = (Get-Date).AddSeconds($HoldSeconds)
    $monitorFailures = 0
    while ((Get-Date) -lt $monitorDeadline) {
        $sample = [ordered]@{at=(Get-Date).ToUniversalTime().ToString('o');http=$false;apEnabled=$null;channel=$null;stationCount=$null;staState='unknown'}
        try {
            $probe = Invoke-WebRequest -WebSession $webSession -UseBasicParsing -Uri 'http://192.168.226.1/' -TimeoutSec 4
            $sample.http = $probe.StatusCode -eq 200 -and $probe.Content -match '<div id="app">'
            $apProbe = Invoke-RestMethod -WebSession $webSession -Uri 'http://192.168.226.1/api/v1/network/hotspot' -TimeoutSec 4
            $sample.apEnabled = $apProbe.data.enabled
            $sample.channel = $apProbe.data.channel
            $sample.stationCount = $apProbe.data.clientCount
            $staProbe = Invoke-RestMethod -WebSession $webSession -Uri 'http://192.168.226.1/api/v1/network/wifi/networks' -TimeoutSec 4
            $sample.staState = $staProbe.status.state
            if (-not $sample.http -or $sample.apEnabled -ne $true -or $sample.stationCount -lt 1) { $monitorFailures++ }
        } catch { $monitorFailures++;$sample.errorClass=$_.Exception.GetType().Name }
        if ($MonitorPath) { [System.IO.File]::AppendAllText([System.IO.Path]::GetFullPath($MonitorPath), (($sample | ConvertTo-Json -Compress) + [Environment]::NewLine)) }
        Start-Sleep -Seconds 1
    }
    if ($monitorFailures) { throw 'Continuous AP HTTP/station verification failed; inspect private monitor output.' }
    Write-Output 'PASS: Windows Wi-Fi association, DHCP, HTTP UI and observed AP station'
} finally {
    & netsh wlan disconnect interface=$Interface | Out-Null
    & netsh wlan delete profile name=$profile interface=$Interface | Out-Null
    Remove-Item -LiteralPath $xmlPath -ErrorAction SilentlyContinue
}
