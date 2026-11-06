$ErrorActionPreference = 'Stop'
$script:workspaceClosed=$false
$utf8=New-Object Text.UTF8Encoding($false)
$pipeReader=New-Object IO.StreamReader([Console]::OpenStandardInput(),$utf8)
$pipeWriter=New-Object IO.StreamWriter([Console]::OpenStandardOutput(),$utf8)
$pipeWriter.AutoFlush=$true
[Console]::SetIn($pipeReader)
[Console]::SetOut($pipeWriter)
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
[System.Windows.Forms.Application]::SetCompatibleTextRenderingDefault($false)
# Compiling the controls runs csc.exe and takes seconds, so the result is kept as a DLL named after
# a hash of the source (TOLK_SHOT_DLL). A DLL that cannot be loaded (AppLocker, a torn file) falls
# back to compiling again, and that to compiling in memory as before. The return value says which,
# so nothing else in here may write to the pipeline.
function LoadControls {
 $refs='System.Windows.Forms','System.Drawing'
 $dll=$env:TOLK_SHOT_DLL
 if($dll){
  if(Test-Path -LiteralPath $dll){
   try{$null=Add-Type -LiteralPath $dll;return 'cache'}catch{}
  }
  try{
   # Compile into a private folder: a concurrent start must never load a half-written file.
   $work=Join-Path (Split-Path -Parent $dll) ('build-'+$PID)
   [void](New-Item -ItemType Directory -Force -Path $work)
   $out=Join-Path $work (Split-Path -Leaf $dll)
   Remove-Item -LiteralPath $out -Force -ErrorAction SilentlyContinue
   $null=Add-Type -ReferencedAssemblies $refs -TypeDefinition $script:controlsSource -OutputAssembly $out -OutputType Library
   $next=$dll+'.'+$PID+'.tmp'
   $null=Copy-Item -LiteralPath $out -Destination $next -Force
   # Move refuses an existing target: another start got there first with the same source.
   try{[IO.File]::Move($next,$dll)}catch{Remove-Item -LiteralPath $next -Force -ErrorAction SilentlyContinue}
   if(!('CaptureDpi' -as [type])){$null=Add-Type -LiteralPath $out}
   return 'compiled'
  }catch{}
 }
 if('CaptureDpi' -as [type]){return 'compiled'}
 $null=Add-Type -ReferencedAssemblies $refs -TypeDefinition $script:controlsSource
 return 'memory'
}
$script:typesStarted=[Diagnostics.Stopwatch]::StartNew()
$script:typesFrom=LoadControls
$script:typesMs=$script:typesStarted.ElapsedMilliseconds
[CaptureDpi]::SetProcessDpiAwarenessContext([IntPtr](-4)) | Out-Null
function Await($operation, $type) {
 $method = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' } | Select-Object -First 1
 $task = $method.MakeGenericMethod($type).Invoke($null, @($operation))
 while (!$task.IsCompleted) {
  [System.Windows.Forms.Application]::DoEvents()
  if ($script:workspaceClosed -or ($form -and $form.IsDisposed)) { throw '截图已取消' }
  [System.Threading.Thread]::Sleep(10)
 }
 $task.GetAwaiter().GetResult()
}

$script:ocrLoaded=$false
function LoadOCR {
 if($script:ocrLoaded){return}
 Add-Type -AssemblyName System.Runtime.WindowsRuntime
[Windows.Media.Ocr.OcrEngine,Windows.Media.Ocr,ContentType=WindowsRuntime] | Out-Null
[Windows.Graphics.Imaging.BitmapDecoder,Windows.Graphics.Imaging,ContentType=WindowsRuntime] | Out-Null
[Windows.Storage.Streams.InMemoryRandomAccessStream,Windows.Storage.Streams,ContentType=WindowsRuntime] | Out-Null
[Windows.Globalization.Language,Windows.Globalization,ContentType=WindowsRuntime] | Out-Null
[Windows.Media.Ocr.OcrResult,Windows.Media.Ocr,ContentType=WindowsRuntime] | Out-Null
[Windows.Graphics.Imaging.SoftwareBitmap,Windows.Graphics.Imaging,ContentType=WindowsRuntime] | Out-Null
 $script:ocrLoaded=$true
}
# Everything above is the same for every screenshot, so Tolk starts this process ahead of time
# and it waits here. OCR is loaded now too; if that fails, the workspace reports it after start.
try{LoadOCR}catch{}
[Console]::Out.WriteLine((@{warm=$true;types=$script:typesFrom;typesMs=$script:typesMs} | ConvertTo-Json -Compress));[Console]::Out.Flush()
# The start message carries what may have changed while waiting: target language and theme.
$startLine=[Console]::In.ReadLine()
if(!$startLine){exit 0}
$shot=$startLine | ConvertFrom-Json

$bounds = [System.Windows.Forms.SystemInformation]::VirtualScreen
$screen = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height)
$graphics = [System.Drawing.Graphics]::FromImage($screen)
$graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
$graphics.Dispose()
$themeName=[string]$shot.theme
if($themeName -ne 'dark' -and $themeName -ne 'light' -and $themeName -ne 'pink'){
 $themeName='light'
 try{ if((Get-ItemPropertyValue 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize' 'AppsUseLightTheme') -eq 0){$themeName='dark'} }catch{}
}
$palette=[ShotPalette]::For($themeName)
$form = New-Object ShotForm
$form.Palette = $palette
$form.Freeze($screen,107)
$form.FormBorderStyle = 'None'
$form.StartPosition = 'Manual'
$form.AutoScaleMode = 'None'
$form.Font = New-Object Drawing.Font('Microsoft YaHei UI',10,[Drawing.FontStyle]::Regular)
$form.Bounds = $bounds
$form.TopMost = $true
$form.ShowInTaskbar = $false
$form.KeyPreview = $true
$form.Cursor = [System.Windows.Forms.Cursors]::Cross
$script:start = $null
$script:selection = [System.Drawing.Rectangle]::Empty
$form.Add_Shown({ $form.Activate() })
$form.Add_FormClosing({$script:workspaceClosed=$true})
$form.Add_KeyDown({ if ($_.KeyCode -eq 'Escape') { $form.Close() } })
$form.Add_MouseDown({
 if ($_.Button -eq 'Right') { if($script:crop -and !$script:working){ResetSelection}else{$form.Close()}; return }
 if ($script:readTask -or $script:crop) { return }
 $script:start = $_.Location
 $form.SelectionScale=[CaptureDpi]::ScaleAt($bounds.X+$_.X,$bounds.Y+$_.Y)
})
$form.Add_MouseMove({
 if ($null -eq $script:start) { return }
 $script:selection = New-Object System.Drawing.Rectangle([Math]::Min($script:start.X,$_.X),[Math]::Min($script:start.Y,$_.Y),[Math]::Abs($_.X-$script:start.X),[Math]::Abs($_.Y-$script:start.Y))
 $form.SetSelection($script:selection)
})

