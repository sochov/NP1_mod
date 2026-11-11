# Synthetic fixture only. No real-device images or audio are used.
param([string]$FFmpeg='ffmpeg', [string]$VideoEncoder='')
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
$output=Join-Path $projectRoot 'tmp/drive-test-fixture.mp4'
New-Item -ItemType Directory -Path (Split-Path -Parent $output) -Force | Out-Null
if(!$VideoEncoder){
 $encoders=(& $FFmpeg -hide_banner -encoders 2>&1 | Out-String)
 $VideoEncoder=if($encoders -match '\blibx264\b'){'libx264'}elseif($encoders -match '\bh264_mf\b'){'h264_mf'}else{throw 'A H.264 encoder (libx264 or h264_mf) is required'}
}
$pixelFormat=if($VideoEncoder -eq 'h264_mf'){'nv12'}else{'yuv420p'}
& $FFmpeg -hide_banner -loglevel error -y -f lavfi -i 'testsrc2=size=320x180:rate=4' -f lavfi -i 'sine=frequency=440:sample_rate=48000' -t 20 -c:v $VideoEncoder -pix_fmt $pixelFormat -b:v 200k -c:a aac -ac 2 $output
if($LASTEXITCODE -ne 0){throw 'Synthetic playback fixture generation failed'}
Write-Host "Created synthetic playback fixture: $output"
