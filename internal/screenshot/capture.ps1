$ErrorActionPreference = 'Stop'
$script:workspaceClosed=$false
[Console]::OutputEncoding = New-Object Text.UTF8Encoding($false)
[Console]::InputEncoding = New-Object Text.UTF8Encoding($false)
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
[System.Windows.Forms.Application]::SetCompatibleTextRenderingDefault($false)
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class CaptureDpi {
 public static System.Threading.Tasks.Task<string> ReadReply() { return System.Threading.Tasks.Task.Run(() => Console.In.ReadLine()); }
 [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr value);
}
'@
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

$bounds = [System.Windows.Forms.SystemInformation]::VirtualScreen
$screen = New-Object System.Drawing.Bitmap($bounds.Width, $bounds.Height)
$graphics = [System.Drawing.Graphics]::FromImage($screen)
$graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
$graphics.Dispose()
$form = New-Object System.Windows.Forms.Form
$form.FormBorderStyle = 'None'
$form.StartPosition = 'Manual'
$form.AutoScaleMode = 'None'
$form.Font = New-Object Drawing.Font('Microsoft YaHei UI',10,[Drawing.FontStyle]::Regular)
$form.Bounds = $bounds
$form.TopMost = $true
$form.ShowInTaskbar = $false
$form.KeyPreview = $true
$form.Cursor = [System.Windows.Forms.Cursors]::Cross
$form.BackgroundImage = $screen
$form.BackgroundImageLayout = 'None'
$script:start = $null
$script:selection = [System.Drawing.Rectangle]::Empty
$form.Add_Shown({ $form.Activate() })
$form.Add_FormClosing({$script:workspaceClosed=$true})
$form.Add_KeyDown({ if ($_.KeyCode -eq 'Escape') { $form.Close() } })
$form.Add_MouseDown({
 if ($_.Button -eq 'Right') { if($script:crop -and !$script:working){ResetSelection}else{$form.Close()}; return }
 if ($script:readTask -or $script:crop) { return }
 $script:start = $_.Location
})
$form.Add_MouseMove({
 if ($null -eq $script:start) { return }
 $script:selection = New-Object System.Drawing.Rectangle([Math]::Min($script:start.X,$_.X),[Math]::Min($script:start.Y,$_.Y),[Math]::Abs($_.X-$script:start.X),[Math]::Abs($_.Y-$script:start.Y))
 $form.Invalidate()
})
$form.Add_Paint({
 $shade = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(100,0,0,0))
 $_.Graphics.FillRectangle($shade,$form.ClientRectangle)
 $shade.Dispose()
 if ($script:selection.Width -gt 0 -and $script:selection.Height -gt 0) {
  $_.Graphics.DrawImage($screen,$script:selection,$script:selection,[System.Drawing.GraphicsUnit]::Pixel)
  $_.Graphics.DrawRectangle([System.Drawing.Pens]::DodgerBlue,$script:selection)
 }
})

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
