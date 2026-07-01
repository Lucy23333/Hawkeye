$ErrorActionPreference = "Stop"

$listenAddress = "0.0.0.0"
$listenPort = 8080
$connectAddress = "192.168.38.129"
$connectPort = 8080
$ruleName = "Hawkeye Backend 8080 NAT Forward"

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Please run this script from an elevated PowerShell window."
}

netsh interface portproxy delete v4tov4 listenaddress=$listenAddress listenport=$listenPort 2>$null | Out-Null
netsh interface portproxy add v4tov4 listenaddress=$listenAddress listenport=$listenPort connectaddress=$connectAddress connectport=$connectPort

netsh advfirewall firewall delete rule name="$ruleName" 2>$null | Out-Null
netsh advfirewall firewall add rule name="$ruleName" dir=in action=allow protocol=TCP localport=$listenPort

netsh interface portproxy show all
