param([string]$Interface = 'Wi-Fi', [switch]$AllowWindowsWiFiChanges)
if(-not $AllowWindowsWiFiChanges){throw 'Windows Wi-Fi changes are disabled; explicit user authorization and opt-in are required.'}
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$access = Get-Content -LiteralPath "$repo/tmp/ap-client-access.json" -Raw | ConvertFrom-Json
$adapter = Get-NetAdapter -Name $Interface
if ($adapter.Status -ne 'Disconnected') { throw 'Use a disconnected Wi-Fi adapter; do not replace an existing connection.' }
$profile = 'ncc-test-' + [guid]::NewGuid().ToString('N')
$ssid = [System.Security.SecurityElement]::Escape($access.ssid)
$key = [System.Security.SecurityElement]::Escape($access.password)
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
    $response = Invoke-WebRequest -UseBasicParsing -Uri 'http://192.168.226.1/' -TimeoutSec 10
    if ($response.StatusCode -ne 200 -or $response.Content -notmatch '<div id="app">') { throw 'AP Web UI was not confirmed.' }
    $stationDeadline = (Get-Date).AddSeconds(7)
    do {
        $status = Invoke-RestMethod -Uri 'http://192.168.226.1/api/v1/network/hotspot' -TimeoutSec 10
        if ($status.data.clientCount -ge 1) { break }
        Start-Sleep -Milliseconds 500
    } while ((Get-Date) -lt $stationDeadline)
    if ($status.data.enabled -ne $true -or $status.data.dhcpReady -ne $true -or $status.data.clientCount -lt 1) { throw 'AP station state was not confirmed.' }
    Write-Output 'PASS: Windows Wi-Fi association, DHCP, HTTP UI and observed AP station'
} finally {
    & netsh wlan disconnect interface=$Interface | Out-Null
    & netsh wlan delete profile name=$profile interface=$Interface | Out-Null
    Remove-Item -LiteralPath $xmlPath -ErrorAction SilentlyContinue
}
