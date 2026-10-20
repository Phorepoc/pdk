# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# install-windows.ps1 -- run PhoneLab at logon and open it to the LAN (run ONCE, elevated):
#   powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
# - builds phonelab.exe at the repository root (needs Go) unless -NoBuild
# - firewall: inbound TCP <Port> allowed on the Private profile only (home/work LAN, not public Wi-Fi)
# - a scheduled task "PhoneLab" starts it at logon (the phone's COM port belongs to the logged-on user's session),
#   restarting it if it dies; logs to %LOCALAPPDATA%\phonelab\phonelab.log
# -Uninstall removes the rule and the task.
param([int]$Port = 8737, [switch]$NoBuild, [switch]$Uninstall, [switch]$TaskOnly, [string]$Config = '')
# -Config: a phonelab.toml of your own (devices, workspace, token); default: the repository's phonelab.toml
# -TaskOnly: rebuild + re-register the logon task only (no admin needed; the firewall rules stay as they are)
$ErrorActionPreference = 'Stop'
$id = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $TaskOnly -and -not (New-Object Security.Principal.WindowsPrincipal $id).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
	throw 'run this from an elevated PowerShell (firewall rules need admin)'
}
$rule = 'PhoneLab (TCP ' + $Port + ')'
$task = 'PhoneLab'
if ($Uninstall) {
	Get-NetFirewallRule -DisplayName $rule -ErrorAction SilentlyContinue | Remove-NetFirewallRule
	Unregister-ScheduledTask -TaskName $task -Confirm:$false -ErrorAction SilentlyContinue
	Write-Host 'PhoneLab: firewall rule and logon task removed'
	return
}
$server = Join-Path $PSScriptRoot '..' | Resolve-Path
$exe = Join-Path $server 'phonelab.exe'
if (-not $NoBuild) {
	Push-Location $server
	# -H windowsgui: no console window (closing one would kill the service); it logs with -log instead
	Get-Process phonelab -ErrorAction SilentlyContinue | Stop-Process -Force
	try { go build -ldflags '-H windowsgui' -o phonelab.exe ./cmd/phonelab; if ($LASTEXITCODE) { throw 'go build failed' } } finally { Pop-Location }
}
if (-not (Test-Path $exe)) { throw "no $exe" }

if (-not $TaskOnly) {
Get-NetFirewallRule -DisplayName $rule -ErrorAction SilentlyContinue | Remove-NetFirewallRule
New-NetFirewallRule -DisplayName $rule -Direction Inbound -Action Allow -Protocol TCP -LocalPort $Port -Profile Private | Out-Null
# also allow the program itself, so Windows does not pop its own "allow access?" prompt on first listen
New-NetFirewallRule -DisplayName ($rule + ' program') -Direction Inbound -Action Allow -Program $exe -Profile Private | Out-Null
Write-Host "PhoneLab: firewall allows TCP $Port on Private networks"
}
$prof = Get-NetConnectionProfile | Select-Object -First 1
if ($prof -and $prof.NetworkCategory -ne 'Private') {
	Write-Warning "your network '$($prof.Name)' is $($prof.NetworkCategory): the LAN cannot reach PhoneLab until it is set to Private (Settings > Network > Properties)"
}

$data = Join-Path $env:LOCALAPPDATA 'phonelab'
New-Item -ItemType Directory -Force $data | Out-Null
$cfg = if ($Config) { Resolve-Path $Config } else { Join-Path $server 'phonelab.toml' }
$cmd = "-config `"$cfg`" -listen 0.0.0.0:$Port -log `"$data\phonelab.log`""
$action = New-ScheduledTaskAction -Execute $exe -Argument $cmd -WorkingDirectory $server
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $id.Name
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit 0 `
	-RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName $task -Action $action -Trigger $trigger -Settings $settings -User $id.Name -RunLevel Limited -Force -ErrorAction Stop | Out-Null
Write-Host "PhoneLab: logon task '$task' registered (start now: Start-ScheduledTask $task)"
$ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.PrefixOrigin -in 'Dhcp', 'Manual' -and $_.IPAddress -notlike '169.*' } | Select-Object -First 1).IPAddress
Write-Host "PhoneLab: open http://${ip}:$Port/ from another machine on the LAN"
