package update

// windowsApplyScript swaps the install dir: wait for the app to exit, mirror a
// backup, mirror the new files over the install dir, keep the Inno Setup
// uninstaller, verify the version, restart unless the user was quitting; on
// any failure restore the backup. It reads environment variables so no path is
// ever interpolated into script text.
const windowsApplyScript = `$SourceDir = [string]$env:TOLK_UPDATE_SOURCE_DIR
$TargetDir = [string]$env:TOLK_UPDATE_TARGET_DIR
$ExeName = [string]$env:TOLK_UPDATE_EXE_NAME
$ProcessId = [int]$env:TOLK_UPDATE_PROCESS_ID
$BackupDir = [string]$env:TOLK_UPDATE_BACKUP_DIR
$LogPath = [string]$env:TOLK_UPDATE_LOG_PATH
$ResultPath = [string]$env:TOLK_UPDATE_RESULT_PATH
$ExpectedVersion = [string]$env:TOLK_UPDATE_EXPECTED_VERSION
# "0" when the user is quitting: swap the files but do not start the app again.
$Relaunch = [string]$env:TOLK_UPDATE_RELAUNCH -ne "0"

$ErrorActionPreference = "Stop"
$backupCompleted = $false

function Write-UpdateLog {
    param([string]$Message)
    $parent = Split-Path -Parent $LogPath
    if ($parent) { [System.IO.Directory]::CreateDirectory($parent) | Out-Null }
    $line = "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') $Message"
    Add-Content -LiteralPath $LogPath -Value $line -Encoding UTF8
}

function Write-UpdateResult {
    param([string]$Status, [string]$Message)
    $parent = Split-Path -Parent $ResultPath
    if ($parent) { [System.IO.Directory]::CreateDirectory($parent) | Out-Null }
    $payload = [PSCustomObject]@{
        status = $Status
        message = $Message
        expected_version = $ExpectedVersion
        log_path = $LogPath
        updated_at = (Get-Date).ToString("o")
    }
    $payload | ConvertTo-Json -Compress | Set-Content -LiteralPath $ResultPath -Encoding UTF8
}

function Invoke-RobocopyChecked {
    param([string]$From, [string]$To, [string]$Phase)
    Write-UpdateLog "$Phase from [$From] to [$To] with /MIR"
    & robocopy $From $To /MIR /IS /R:3 /W:1 /NFL /NDL /NJH /NJS /NP
    $code = $LASTEXITCODE
    Write-UpdateLog "$Phase robocopy exit code: $code"
    if ($code -gt 7) { throw "$Phase failed with robocopy exit code $code" }
}

function Restore-UninstallerFiles {
    param([string]$From, [string]$To)
    foreach ($pattern in @("unins*.exe", "unins*.dat", "unins*.msg")) {
        Get-ChildItem -LiteralPath $From -Filter $pattern -File -ErrorAction SilentlyContinue |
            ForEach-Object {
                Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $To $_.Name) -Force
                Write-UpdateLog "Preserved uninstaller file: $($_.Name)"
            }
    }
}

function Assert-ExpectedVersion {
    param([string]$Directory, [string]$Phase)
    if ([string]::IsNullOrWhiteSpace($ExpectedVersion)) { return }
    $manifestPath = Join-Path $Directory "update_manifest.json"
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
        throw "$Phase version verification failed: update_manifest.json is missing"
    }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $actual = [string]$manifest.app_version
    if ($actual -ne $ExpectedVersion) {
        throw "$Phase version verification failed: expected $ExpectedVersion but found $actual"
    }
    Write-UpdateLog "$Phase version verified: $actual"
}

try {
    Write-UpdateLog "Update started."
    if (-not (Test-Path -LiteralPath $SourceDir -PathType Container)) { throw "SourceDir not found: $SourceDir" }
    if (-not (Test-Path -LiteralPath $TargetDir -PathType Container)) { throw "TargetDir not found: $TargetDir" }
    $newExe = Join-Path $SourceDir $ExeName
    if (-not (Test-Path -LiteralPath $newExe -PathType Leaf)) { throw "New executable not found: $newExe" }
    Assert-ExpectedVersion -Directory $SourceDir -Phase "source"

    $deadline = (Get-Date).AddSeconds(60)
    $process = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
    while ($null -ne $process) {
        if ((Get-Date) -gt $deadline) {
            Write-UpdateLog "Timed out waiting for process $ProcessId to exit; forcing termination."
            Stop-Process -InputObject $process -Force -ErrorAction Stop
            Start-Sleep -Milliseconds 800
            break
        }
        Start-Sleep -Milliseconds 500
        $process = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
    }
    if ($null -ne (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) {
        throw "Process $ProcessId is still running after forced termination."
    }

    # The supervised GUI exits first; release its launcher before replacing it.
    $supervisorId = 0
    if ([int]::TryParse($env:TOLK_SUPERVISOR_PID, [ref]$supervisorId) -and $supervisorId -gt 0) {
        $supervisor = Get-Process -Id $supervisorId -ErrorAction SilentlyContinue
        if ($null -ne $supervisor -and $supervisor.Path -eq (Join-Path $TargetDir "Tolk.exe")) {
            if (-not $supervisor.WaitForExit(10000)) {
                Write-UpdateLog "Stopping verified launcher after GUI termination; queued reports remain on disk."
                Stop-Process -InputObject $supervisor -Force -ErrorAction Stop
                if (-not $supervisor.WaitForExit(5000)) { throw "Launcher has not exited; update cancelled." }
            }
        }
    }
    if (Test-Path -LiteralPath $BackupDir) { Remove-Item -LiteralPath $BackupDir -Recurse -Force }
    [System.IO.Directory]::CreateDirectory($BackupDir) | Out-Null
    Invoke-RobocopyChecked -From $TargetDir -To $BackupDir -Phase "backup"
    $backupCompleted = $true
    Invoke-RobocopyChecked -From $SourceDir -To $TargetDir -Phase "update"
    Restore-UninstallerFiles -From $BackupDir -To $TargetDir

    $targetExe = Join-Path $TargetDir $ExeName
    if (-not (Test-Path -LiteralPath $targetExe -PathType Leaf)) { throw "Updated executable not found: $targetExe" }
    Assert-ExpectedVersion -Directory $TargetDir -Phase "target"

    Write-UpdateResult -Status "success" -Message "Update completed."
    if ($Relaunch) {
        Start-Process -FilePath $targetExe -WorkingDirectory $TargetDir -ArgumentList "--update-restart"
        Write-UpdateLog "Update completed and app restarted."
    }
    else {
        Write-UpdateLog "Update completed."
    }
    exit 0
}
catch {
    Write-UpdateLog "Update failed: $($_.Exception.Message)"
    $restored = $false
    try {
        if ($backupCompleted -and (Test-Path -LiteralPath $BackupDir -PathType Container)) {
            Invoke-RobocopyChecked -From $BackupDir -To $TargetDir -Phase "restore"
            $restored = Test-Path -LiteralPath (Join-Path $TargetDir $ExeName) -PathType Leaf
            Write-UpdateLog "Restore completed."
        }
        else {
            Write-UpdateLog "Backup did not complete; target directory was left unchanged."
            $restored = Test-Path -LiteralPath (Join-Path $TargetDir $ExeName) -PathType Leaf
        }
    }
    catch {
        Write-UpdateLog "Restore failed: $($_.Exception.Message)"
        $restored = $false
    }
    if ($restored -and $Relaunch) {
        Start-Process -FilePath (Join-Path $TargetDir $ExeName) -WorkingDirectory $TargetDir -ArgumentList "--update-restart"
        Write-UpdateLog "Restarted previous version."
    }
    Write-UpdateResult -Status "failed" -Message $_.Exception.Message
    exit 1
}
`
