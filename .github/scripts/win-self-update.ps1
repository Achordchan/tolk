# Helpers for .github/workflows/win-self-update.yml (dot-sourced by its steps).

function Invoke-Rel {
    & (Join-Path $env:ROOT "rel.exe") @args
    if ($LASTEXITCODE -ne 0) { throw "tolk-release $($args[0]) 失败" }
}

# Publish-Version makes version $v the latest release on the local server.
function Publish-Version([string]$v) {
    $dist = Join-Path $env:ROOT "dist"
    Get-ChildItem $dist -File | Remove-Item -Force
    $pub = (Get-Content (Join-Path $env:ROOT "keys\win.pub") -Raw).Trim()
    Invoke-Rel winpack -single-executable -exe (Join-Path $env:ROOT "v$v\Tolk.exe") -version $v -out $dist
    Invoke-Rel winsign -zip (Join-Path $dist "Tolk-update-windows_$v.zip") -version $v -key (Join-Path $env:ROOT "keys\win.pem") -pub $pub
    Invoke-Rel latest -version $v -dir $dist -base "http://127.0.0.1:8765/dl/" -out (Join-Path $dist "latest.json")
}

# Invoke-Update runs the install's Tolk.exe as the app would update itself and
# waits for the swap script's result. It returns the program's log.
function Invoke-Update([string]$Install, [string]$Relaunch) {
    $env:TOLK_E2E_LATEST = "http://127.0.0.1:8765/dl/latest.json"
    $env:TOLK_E2E_ORIGIN = "http://127.0.0.1:8765/dl/"
    $env:TOLK_E2E_MIRROR = "http://127.0.0.1:8765/m/"
    $env:TOLK_E2E_WINDOWS_KEY = (Get-Content (Join-Path $env:ROOT "keys\win.pub") -Raw).Trim()
    $env:TOLK_E2E_RELAUNCH = $Relaunch
    $state = Join-Path $env:TOLK_HOME "update_state"
    $res = Join-Path $state "last_update_result.json"
    Remove-Item $res -ErrorAction SilentlyContinue
    $err = Join-Path $env:ROOT "run.log"
    $p = Start-Process -FilePath (Join-Path $Install "Tolk.exe") -NoNewWindow -Wait -PassThru -RedirectStandardError $err
    $out = Get-Content $err -Raw
    Write-Host "---- Tolk.exe ----"
    Write-Host $out
    if ($p.ExitCode -ne 0) { throw "Tolk.exe 退出码 $($p.ExitCode)" }
    $deadline = (Get-Date).AddMinutes(5)
    while (-not (Test-Path $res) -and (Get-Date) -lt $deadline) { Start-Sleep 1 }
    Write-Host "---- update.log ----"
    Get-Content (Join-Path $state "update.log") -ErrorAction SilentlyContinue | Write-Host
    Write-Host "---- result ----"
    Get-Content $res -ErrorAction SilentlyContinue | Write-Host
    if (-not (Test-Path $res)) { throw "替换脚本没有写出结果" }
    if ((Get-Content $res -Raw | ConvertFrom-Json).status -ne "success") { throw "替换失败" }
    return $out
}

# Assert-Swapped checks the install now holds version $v's program.
function Assert-Swapped([string]$Install, [string]$Version) {
    $got = (Get-FileHash (Join-Path $Install "Tolk.exe")).Hash
    $want = (Get-FileHash (Join-Path $env:ROOT "v$Version\Tolk.exe")).Hash
    if ($got -ne $want) { throw "安装目录里的主程序不是 v$Version" }
    Write-Host "已换成 v$Version"
}
