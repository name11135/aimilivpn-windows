$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath($PSScriptRoot)
$exe = Join-Path $root 'aimilivpn.exe'
$stateDir = Join-Path $root 'runtime\state'
$pidFile = Join-Path $stateDir 'service.pid'
$tokenFile = Join-Path $stateDir 'control.token'
if (-not (Test-Path -LiteralPath $exe)) { throw "Executable missing: $exe. Run Build.cmd first." }
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    $args = "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`""
    $child = Start-Process -FilePath 'powershell.exe' -Verb RunAs -ArgumentList $args -Wait -PassThru
    exit $child.ExitCode
}
function Get-AimiliPid {
    if (-not (Test-Path -LiteralPath $pidFile)) { return $null }
    $raw = (Get-Content -Raw -LiteralPath $pidFile).Trim(); $servicePid = 0
    if (-not [int]::TryParse($raw, [ref]$servicePid) -or $servicePid -le 0) { return $null }
    $p = Get-Process -Id $servicePid -ErrorAction SilentlyContinue
    if ($p -and $p.ProcessName -eq 'aimilivpn') { return $servicePid }
    return $null
}
function Stop-ExistingAimili {
    if (Test-Path -LiteralPath $tokenFile) {
        try {
            $token = (Get-Content -Raw -LiteralPath $tokenFile).Trim()
            Invoke-RestMethod -Method Post 'http://127.0.0.1:8686/api/shutdown' -Headers @{ 'X-Aimili-Token' = $token } -Body '{}' -ContentType 'application/json' -TimeoutSec 3 | Out-Null
        } catch {}
    }
    $deadline = (Get-Date).AddSeconds(25)
    while ((Get-AimiliPid) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 250 }
    if (Get-AimiliPid) { throw 'Existing Aimili service did not stop.' }
}
Stop-ExistingAimili
$conflicts = @()
foreach ($port in @(7928, 8686, 7929)) {
    foreach ($listener in @(Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue)) {
        $owner = Get-Process -Id $listener.OwningProcess -ErrorAction SilentlyContinue
        if ($owner -and $owner.ProcessName -ne 'aimilivpn') { $conflicts += "Port $port is used by $($owner.ProcessName) PID $($listener.OwningProcess)" }
    }
}
if ($conflicts.Count -gt 0) { throw ($conflicts -join '; ') }
$proc = Start-Process -FilePath $exe -WorkingDirectory $root -WindowStyle Hidden -PassThru
$deadline = (Get-Date).AddSeconds(45); $ready = $false
while ((Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 500
    if ($proc.HasExited) { throw "Service exited with code $($proc.ExitCode). See $root\logs\service.log" }
    try {
        $token = (Get-Content -Raw -LiteralPath $tokenFile -ErrorAction Stop).Trim()
        $status = Invoke-RestMethod 'http://127.0.0.1:8686/api/status' -Headers @{ 'X-Aimili-Token' = $token } -TimeoutSec 2
        if ($status.subscription -eq 'http://127.0.0.1:7929/clash') { $ready = $true; break }
    } catch {}
}
if (-not $ready) { throw "Service readiness timeout. See $root\logs\service.log" }
Start-Process 'http://127.0.0.1:8686/'
Write-Host 'Aimili started successfully.'

