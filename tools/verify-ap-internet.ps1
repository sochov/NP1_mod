param([string]$Interface='Wi-Fi',[Parameter(Mandatory=$true)][string]$AccessFile,[Parameter(Mandatory=$true)][string]$ResultFile)
$ErrorActionPreference='Stop'
$profile='ncc-test-'+[guid]::NewGuid().ToString('N')
$xmlPath=Join-Path (Split-Path -Parent $ResultFile) ($profile+'.xml')
$result=@{passed=$false;association=$false;dhcpGateway=$false;dhcpDNS=$false;dnsUDP=$false;dnsTCP=$false;https=$false;httpUI=$false;clientObserved=$false}
try {
 $adapter=Get-NetAdapter -Name $Interface
 if($adapter.Status -ne 'Disconnected'){throw 'Use a disconnected Wi-Fi adapter; existing connections are preserved.'}
 $access=Get-Content -LiteralPath $AccessFile -Raw | ConvertFrom-Json
 $ssid=[System.Security.SecurityElement]::Escape($access.ssid)
 $key=[System.Security.SecurityElement]::Escape($access.password)
 $xml=@"
<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1"><name>$profile</name><SSIDConfig><SSID><name>$ssid</name></SSID></SSIDConfig><connectionType>ESS</connectionType><connectionMode>manual</connectionMode><MSM><security><authEncryption><authentication>WPA2PSK</authentication><encryption>AES</encryption><useOneX>false</useOneX></authEncryption><sharedKey><keyType>passPhrase</keyType><protected>false</protected><keyMaterial>$key</keyMaterial></sharedKey></security></MSM></WLANProfile>
"@
 [System.IO.File]::WriteAllText($xmlPath,$xml,[System.Text.UTF8Encoding]::new($false))
 & netsh wlan add profile filename=$xmlPath interface=$Interface user=current | Out-Null
 if($LASTEXITCODE -ne 0){throw 'Temporary Wi-Fi profile creation failed.'}
 & netsh wlan connect name=$profile interface=$Interface | Out-Null
 if($LASTEXITCODE -ne 0){throw 'Temporary AP association request failed.'}
 $deadline=(Get-Date).AddSeconds(40)
 do {Start-Sleep -Milliseconds 500;$address=Get-NetIPAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {$_.IPAddress -like '192.168.226.*' -and $_.AddressState -eq 'Preferred'} | Select-Object -First 1}while(-not $address -and (Get-Date) -lt $deadline)
 if(-not $address){throw 'AP DHCP address was not observed.'}
 $result.association=$true
 $gateway=Get-NetRoute -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object {$_.NextHop -eq '192.168.226.1'}
 if(-not $gateway){throw 'DHCP gateway is not the AP.'};$result.dhcpGateway=$true
 $dns=(Get-DnsClientServerAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4).ServerAddresses
 if($dns -notcontains '192.168.226.1'){throw 'DHCP DNS is not the AP.'};$result.dhcpDNS=$true
 $response=Invoke-WebRequest -UseBasicParsing -Uri 'http://192.168.226.1/' -TimeoutSec 10
 if($response.StatusCode -ne 200 -or $response.Content -notmatch '<div id="app">'){throw 'AP HTTP UI failed.'};$result.httpUI=$true
 $deadline=(Get-Date).AddSeconds(15)
 do {$status=Invoke-RestMethod -Uri 'http://192.168.226.1/api/v1/network/hotspot' -TimeoutSec 10;if($status.data.clientCount -ge 1){break};Start-Sleep -Milliseconds 500}while((Get-Date) -lt $deadline)
 if($status.data.clientCount -lt 1){throw 'Owned AP station/sharing read-back failed.'}
 $result.clientObserved=$true;$result.upstream=$access.upstream
 # Both queries explicitly target the AP, rather than Windows' wired resolver.
 $answers=Resolve-DnsName example.com -Server '192.168.226.1' -Type A -DnsOnly -NoHostsFile -ErrorAction Stop
 $target=$answers | Where-Object {$_.Type -eq 'A' -and $_.IPAddress} | Select-Object -First 1 -ExpandProperty IPAddress
 if(-not $target){throw 'AP UDP DNS did not return IPv4.'};$result.dnsUDP=$true
 $tcp=Resolve-DnsName example.com -Server '192.168.226.1' -Type A -DnsOnly -NoHostsFile -TcpOnly -ErrorAction Stop
 if(-not ($tcp | Where-Object {$_.Type -eq 'A' -and $_.IPAddress})){throw 'AP TCP DNS did not return IPv4.'};$result.dnsTCP=$true
 $parsed=[System.Net.IPAddress]::Parse($target)
 if($parsed.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetwork -or $target -match '^(0\.|10\.|127\.|169\.254\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.168\.)'){throw 'Test DNS returned a non-public target.'}
 # IP_UNICAST_IF selects Wi-Fi even while Ethernet remains the PC default.
 # The socket also binds the AP-assigned source; no host route is changed.
 $socket=[System.Net.Sockets.Socket]::new([System.Net.Sockets.AddressFamily]::InterNetwork,[System.Net.Sockets.SocketType]::Stream,[System.Net.Sockets.ProtocolType]::Tcp)
 try {
  $socket.SendTimeout=15000;$socket.ReceiveTimeout=15000
  $socket.SetSocketOption([System.Net.Sockets.SocketOptionLevel]::IP,[System.Net.Sockets.SocketOptionName]31,([int][System.Net.IPAddress]::HostToNetworkOrder([int]$adapter.ifIndex)))
  if($socket.GetSocketOption([System.Net.Sockets.SocketOptionLevel]::IP,[System.Net.Sockets.SocketOptionName]31) -ne $adapter.ifIndex){throw 'Outgoing AP interface read-back failed.'}
  $socket.Bind([System.Net.IPEndPoint]::new([System.Net.IPAddress]::Parse($address.IPAddress),0))
  $connect=$socket.BeginConnect($target,443,$null,$null)
  if(-not $connect.AsyncWaitHandle.WaitOne(20000)){throw 'AP HTTPS socket connect timed out.'}
  $socket.EndConnect($connect)
  $stream=[System.Net.Sockets.NetworkStream]::new($socket,$false)
  $tls=[System.Net.Security.SslStream]::new($stream,$false)
  try {
   $tls.ReadTimeout=20000;$tls.WriteTimeout=20000
   # Windows validates the server certificate/hostname using its native trust
   # store. No validation callback, bypass or TLS verification exception is used.
   $tls.AuthenticateAsClient('example.com')
   $request=[System.Text.Encoding]::ASCII.GetBytes("GET / HTTP/1.1`r`nHost: example.com`r`nConnection: close`r`n`r`n")
   $tls.Write($request,0,$request.Length);$tls.Flush()
   $buffer=New-Object byte[] 4096;$responseText=''
   while($responseText -notmatch "`r`n`r`n" -and $responseText.Length -lt 32768){$count=$tls.Read($buffer,0,$buffer.Length);if($count -eq 0){break};$responseText += [System.Text.Encoding]::ASCII.GetString($buffer,0,$count)}
   if($responseText -notmatch '^HTTP/1\.[01] 200 '){throw 'HTTPS over the AP did not return HTTP 200.'}
   @{passed=$true;interfaceBound=$true;tlsVerified=$true;nativeWindowsTrust=$true} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path (Split-Path -Parent $ResultFile) 'https-proof.json') -Encoding UTF8
  } finally {$tls.Dispose();$stream.Dispose()}
 } finally {$socket.Dispose()}
 $result.https=$true;$result.passed=$true
}catch{$result.error=$_.Exception.Message}finally{
 & netsh wlan disconnect interface=$Interface | Out-Null
 & netsh wlan delete profile name=$profile interface=$Interface | Out-Null
 Remove-Item -LiteralPath $xmlPath -ErrorAction SilentlyContinue
 $result | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $ResultFile -Encoding UTF8
}
if(-not $result.passed){Write-Output 'FAIL: AP internet check; private result saved';exit 1}
Write-Output ('PASS: AP DHCP gateway/DNS, UDP/TCP DNS and HTTPS via '+$result.upstream)
