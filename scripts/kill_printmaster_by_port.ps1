Param(
    [int[]]$Ports = @(8080,8443,9443)
)

# Kill by known process names/paths (printmaster, debug_bin, dlv)
$killedDetails = @()
$killedIds = @()
try {
    $candidates = Get-Process -ErrorAction SilentlyContinue | Where-Object {
        ($_.ProcessName -like '*printmaster*') -or
        ($_.Path -and $_.Path -like '*printmaster*') -or
        ($_.ProcessName -like '*debug_bin*') -or
        ($_.ProcessName -eq 'dlv')
    }
    foreach ($p in $candidates) {
        try {
            Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
            $killedDetails += [pscustomobject]@{ Id = $p.Id; ProcessName = $p.ProcessName; Path = $p.Path }
            $killedIds += $p.Id
        } catch {}
    }
} catch {}

# Find PIDs listening on the configured ports and kill them.
$portPids = @()
try {
    $conns = Get-NetTCPConnection -LocalPort $Ports -ErrorAction Stop
    if ($conns) {
        $portPids = $conns | Where-Object {$_.OwningProcess -ne $null} | Select-Object -ExpandProperty OwningProcess -Unique
    }
} catch {
    # Fallback to parsing netstat output if Get-NetTCPConnection is unavailable
    try {
        $lines = netstat -aon | Select-String ("(:" + ($Ports -join "|:") + ")")
        foreach ($line in $lines) {
            $text = $line.ToString().Trim()
            # try split by whitespace and take last token as PID
            $parts = $text -split '\s+'
            if ($parts.Length -gt 0) {
                $pidCandidate = $parts[-1]
                if ($pidCandidate -match '^[0-9]+$') { $portPids += [int]$pidCandidate }
            }
        }
    } catch {}
}

$portPids = $portPids | Where-Object {$_ -ne $null} | Select-Object -Unique
foreach ($listenerPid in $portPids) {
    if (-not ($killedIds -contains $listenerPid)) {
        try {
            $proc = Get-Process -Id $listenerPid -ErrorAction SilentlyContinue
            if ($proc) {
                Stop-Process -Id $listenerPid -Force -ErrorAction SilentlyContinue
                $killedDetails += [pscustomobject]@{ Id = $proc.Id; ProcessName = $proc.ProcessName; Path = $proc.Path }
                $killedIds += $proc.Id
            } else {
                # Still attempt to stop by PID even if Get-Process couldn't fetch details
                Stop-Process -Id $listenerPid -Force -ErrorAction SilentlyContinue
                $killedDetails += [pscustomobject]@{ Id = $listenerPid; ProcessName = '<unknown>'; Path = '' }
                $killedIds += $listenerPid
            }
        } catch {}
    }
}

if ($killedDetails.Count -gt 0) {
    Write-Host "Killed processes:" -ForegroundColor Green
    $killedDetails | Format-Table Id, ProcessName, Path -AutoSize
    Write-Host "Summary: $($killedIds -join ', ')" -ForegroundColor Green
} else {
    Write-Host "No matching PrintMaster processes or port listeners found" -ForegroundColor Yellow
}

# Exit with success
exit 0
