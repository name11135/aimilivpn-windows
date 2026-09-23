$ErrorActionPreference='Stop'
$tokenFile=Join-Path $PSScriptRoot 'runtime\state\control.token'
$pidFile=Join-Path $PSScriptRoot 'runtime\state\service.pid'
$serviceId=0
if(Test-Path -LiteralPath $pidFile){[int]::TryParse((Get-Content -Raw -LiteralPath $pidFile).Trim(),[ref]$serviceId)|Out-Null}
if(-not(Test-Path -LiteralPath $tokenFile)){Write-Host 'AimiliVPN is not running.';exit 0}
$token=(Get-Content -Raw -LiteralPath $tokenFile).Trim()
$requestError=$null
try {
 Invoke-RestMethod -Method Post 'http://127.0.0.1:8686/api/shutdown' -Headers @{'X-Aimili-Token'=$token} -Body '{}' -ContentType application/json -TimeoutSec 5 | Out-Null
}catch{$requestError=$_}
$deadline=(Get-Date).AddSeconds(25)
do {
 Start-Sleep -Milliseconds 300
 $running=$null
 if($serviceId -gt 0){$running=Get-Process -Id $serviceId -ErrorAction SilentlyContinue | Where-Object ProcessName -eq 'aimilivpn'}
}while($running -and (Get-Date)-lt $deadline)
if($running){if($requestError){throw $requestError};throw 'Service has not stopped yet. See logs/service.log.'}
Write-Host 'AimiliVPN stopped. Its VPN child is stopped; other programs are unchanged.'

