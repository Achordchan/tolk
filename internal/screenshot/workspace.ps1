# Loaded after capture.ps1 initializes the frozen screen, selection form and OCR bridge.
$script:crop = $null
$script:translated = $null
$script:blocks = @()
$script:requestId = 0
$script:readTask = $null
$script:pins = New-Object System.Collections.Generic.List[System.Windows.Forms.Form]
$script:fullTranslation = ''
$script:working = $false
$bar = New-Object ShotBar($form,$palette)
$form.Controls.Add($bar)
function NewButton($label,$kind,$glyph) {
 $button=New-Object ShotButton
 $button.Text=$label;$button.Kind=$kind;$button.Glyph=$glyph
 return $button
}
$source = New-Object ShotPicker
$source.Label='源语言'
[void]$source.Items.Add('自动检测')
$script:sourceTags=@('')
$source.SelectedIndex=0
$bar.AddTop($source)
$bar.AddTop((New-Object ShotArrow))
$target = New-Object ShotPicker
$target.Label='目标语言'
foreach ($name in ($env:TOLK_SHOT_LANGUAGES | ConvertFrom-Json)) { [void]$target.Items.Add([string]$name) }
$target.SelectedItem=$env:TOLK_SHOT_TARGET
if ($target.SelectedIndex -lt 0) { $target.SelectedIndex=0 }
$bar.AddTop($target)
$translate=NewButton '翻译' ([ShotKind]::Primary) 'translate'
$bar.SetAction($translate)
$compare=New-Object ShotToggle
$compare.Text='原文对照';$compare.Enabled=$false
$bar.AddBottomLeft($compare)
$extract=NewButton '提取原文' ([ShotKind]::Ghost) 'extract'
$bar.AddBottomLeft($extract)
$viewText=NewButton '查看译文' ([ShotKind]::Ghost) 'text'
$viewText.Enabled=$false
$bar.AddBottomLeft($viewText)
$pin=NewButton '钉到屏幕' ([ShotKind]::Ghost) 'pin'
$bar.AddBottomLeft($pin)
$reselect=NewButton '重新框选' ([ShotKind]::Ghost) 'reselect'
$bar.AddBottomLeft($reselect)
$close=NewButton '退出' ([ShotKind]::Quiet) ''
$bar.AddBottomRight($close)
$finish=NewButton '复制完成' ([ShotKind]::Soft) 'check'
$bar.AddBottomRight($finish)
$script:workArea=[Drawing.Rectangle]::Empty
function CursorArea {
 $p=[Windows.Forms.Cursor]::Position
 $area=[Windows.Forms.Screen]::FromPoint($p).WorkingArea
 $area.Offset(-$bounds.Left,-$bounds.Top)
 return $area
}
function ShowHint($title) {
 $p=[Windows.Forms.Cursor]::Position
 $form.SetHint($title,'Esc 退出。截图和识别在本机完成，仅识别文字发送给当前翻译服务。',(CursorArea),[CaptureDpi]::ScaleAt($p.X,$p.Y))
}
function Status($text,$failed=$false) {
 if(!$bar.Visible){ShowHint $text;return}
 $bar.SetStatus($text,[bool]$failed)
 $bar.Place($script:selection,$script:workArea)
}
function Failed($text) {Status $text $true}
function SyncOverlay {
 if($script:crop -and $script:translated -and !$compare.Checked){$form.SetOverlay($script:translated)}else{$form.SetOverlay($null)}
}
function Busy($on) {
 $script:working=$on
 $translate.Enabled=!$on;$extract.Enabled=!$on;$finish.Enabled=!$on;$pin.Enabled=!$on
 $source.Enabled=!$on;$target.Enabled=!$on
 $reselect.Enabled=!$on
 $viewText.Enabled=(!$on -and $script:fullTranslation)
 $compare.Enabled=(!$on -and $null -ne $script:translated)
}
function ClearTranslation {
 $script:fullTranslation=''
 $viewText.Enabled=$false
 $form.SetOverlay($null)
 if ($script:translated) {$script:translated.Dispose();$script:translated=$null}
 $compare.Checked=$false;$compare.Enabled=$false
 $translate.Text='翻译'
}
function Recognize {
 LoadOCR
 if ($script:blocks.Count -gt 0) { return }
 $engine=[Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
 if ($source.SelectedIndex -gt 0) {
  $language=New-Object Windows.Globalization.Language($script:sourceTags[$source.SelectedIndex])
  $engine=[Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($language)
 }
 if (!$engine) {throw '没有可用的 OCR 语言包，请在 Windows 语言设置中安装。'}
 if ($script:crop.Width -gt [Windows.Media.Ocr.OcrEngine]::MaxImageDimension -or $script:crop.Height -gt [Windows.Media.Ocr.OcrEngine]::MaxImageDimension) {throw '选区过大，请缩小选区后重试。'}
 $memory=New-Object IO.MemoryStream
 try {
  $script:crop.Save($memory,[Drawing.Imaging.ImageFormat]::Png);$memory.Position=0
  $stream=[IO.WindowsRuntimeStreamExtensions]::AsRandomAccessStream($memory)
  try {
   $decoder=Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
   $bitmap=Await ($decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
   try {
    $recognized=Await ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
    $script:blocks=@(foreach($line in $recognized.Lines){
     $left=[double]::MaxValue;$top=[double]::MaxValue;$right=0;$bottom=0
     foreach($word in $line.Words){$r=$word.BoundingRect;$left=[Math]::Min($left,$r.X);$top=[Math]::Min($top,$r.Y);$right=[Math]::Max($right,$r.X+$r.Width);$bottom=[Math]::Max($bottom,$r.Y+$r.Height)}
     if ($line.Text.Trim() -and $right -gt $left) { [PSCustomObject]@{Text=$line.Text;X=$left;Y=$top;W=$right-$left;H=$bottom-$top} }
    })
   } finally {$bitmap.Dispose()}
  } finally {$stream.Dispose()}
 } finally {$memory.Dispose()}
 $script:blocks=@(GroupParagraphs $script:blocks)
 if (!$script:blocks.Count) {throw '未识别到文字。请重新框选或切换源语言后重试。'}
}
function Render($texts) {
 $image=New-Object Drawing.Bitmap($script:crop)
 $g=[Drawing.Graphics]::FromImage($image)
 $g.TextRenderingHint=[Drawing.Text.TextRenderingHint]::AntiAliasGridFit
 try {
  for($i=0;$i -lt $script:blocks.Count;$i++) {
   $block=$script:blocks[$i]
   $x=[Math]::Max(0,[int]$block.X-2);$y=[Math]::Max(0,[int]$block.Y-2)
   $w=[Math]::Min($image.Width-$x,[int]$block.W+4);$h=[Math]::Min($image.Height-$y,[int]$block.H+4)
   $color=$script:crop.GetPixel($x,$y)
   $background=New-Object Drawing.SolidBrush($color)
   $foreground=if ($color.GetBrightness() -gt 0.5){[Drawing.Brushes]::Black}else{[Drawing.Brushes]::White}
   $rect=New-Object Drawing.RectangleF($x,$y,$w,$h)
   $size=[Math]::Max(6,[Math]::Min(32,$block.LineHeight*0.8))
   $font=New-Object Drawing.Font('Microsoft YaHei',[single]$size,[Drawing.FontStyle]::Regular,[Drawing.GraphicsUnit]::Pixel)
   while($size -gt 6) {
    $measured=$g.MeasureString([string]$texts[$i],$font,[int]$w)
    if($measured.Height -le $h -and $measured.Width -le $w){break}
    $font.Dispose();$size-=0.5;$font=New-Object Drawing.Font('Microsoft YaHei',[single]$size,[Drawing.FontStyle]::Regular,[Drawing.GraphicsUnit]::Pixel)
   }
   $measured=$g.MeasureString([string]$texts[$i],$font,[int]$w)
   if ($measured.Height -gt $h -or $measured.Width -gt $w) {
    $font.Dispose();$background.Dispose()
    throw '译文长度超出原文字区域，已保留原图。可点击“查看译文”阅读和复制完整结果。'
   }
   $g.FillRectangle($background,$rect);$g.DrawString([string]$texts[$i],$font,$foreground,$rect)
   $font.Dispose();$background.Dispose()
  }
 } catch {$image.Dispose();throw} finally {$g.Dispose()}
 return $image
}
$compare.Add_CheckedChanged({SyncOverlay})
$translate.Add_Click({
 try {
  Busy $true;Status '正在识别文字…';$form.Refresh()
  Recognize
  if($script:workspaceClosed){return}
  ClearTranslation
  $script:requestId++
  # OCR language selection controls recognition; translation auto-detects the recognized text.
  $request=@{id=$script:requestId;source='自动检测';target=[string]$target.SelectedItem;blocks=@($script:blocks | ForEach-Object {$_.Text})}
  $json=$request | ConvertTo-Json -Compress -Depth 5
  if($script:workspaceClosed){return}
  [Console]::Out.WriteLine($json);[Console]::Out.Flush()
  $script:readTask=[CaptureDpi]::ReadReply()
  Status ('正在翻译 '+$script:blocks.Count+' 个文字区域…')
 } catch {Failed $_.Exception.Message;Busy $false}
})
$extract.Add_Click({
 try{Busy $true;Status '正在识别文字…';$form.Refresh();Recognize;[Windows.Forms.Clipboard]::SetText(($script:blocks | ForEach-Object {$_.Text}) -join "`r`n");Status '原文已复制'}catch{Failed $_.Exception.Message}finally{Busy $false}
})
function ExportImage {if($script:translated -and !$compare.Checked){return $script:translated};return $script:crop}
$viewText.Add_Click({
 try {
 $scale=[CaptureDpi]::ScaleAt($bounds.X+$script:selection.X,$bounds.Y+$script:selection.Y)
 $textWindow=New-Object Windows.Forms.Form
 $textWindow.Text='完整译文';$textWindow.AutoScaleMode='None'
 $textWindow.ClientSize=New-Object Drawing.Size([int](560*$scale),[int](400*$scale))
 $textWindow.StartPosition='CenterParent';$textWindow.BackColor=$palette.Card;$textWindow.KeyPreview=$true;$textWindow.ShowInTaskbar=$false
 # The workspace is topmost; this window has to stay above it.
 $textWindow.TopMost=$true
 $textWindow.Padding=New-Object Windows.Forms.Padding([int](18*$scale))
 $textWindow.Add_KeyDown({if($_.KeyCode -eq 'Escape'){$this.Close()}})
 $textBox=New-Object Windows.Forms.TextBox
 $textBox.Multiline=$true;$textBox.ReadOnly=$true;$textBox.ScrollBars='Vertical';$textBox.Dock='Fill';$textBox.BorderStyle='None'
 $textBox.BackColor=$palette.Card;$textBox.ForeColor=$palette.Fg;$textBox.Text=$script:fullTranslation
 $textBox.Font=[ShotPaint]::FontPx(15*$scale,[Drawing.FontStyle]::Regular)
 $textWindow.Controls.Add($textBox)
 $textWindow.Add_Shown({$textBox.SelectionStart=0;$textBox.SelectionLength=0})
 [CaptureDpi]::DarkCaption($textWindow.Handle,$palette.Dark)
 try{[void]$textWindow.ShowDialog($form)}finally{$textBox.Font.Dispose();$textWindow.Dispose()}
 } catch {Failed ('无法打开译文窗口：'+$_.Exception.Message)}
})
$finish.Add_Click({
 try {[Windows.Forms.Clipboard]::SetImage((ExportImage));$form.Close()}catch{Failed ('复制失败：'+$_.Exception.Message)}
})
$pin.Add_Click({
 $window=New-Object Windows.Forms.Form
 $window.Text='Tolk 截图 · Esc 关闭';$window.TopMost=$true;$window.KeyPreview=$true
 $window.StartPosition='Manual';$window.Location=New-Object Drawing.Point(($bounds.X+$script:selection.X),($bounds.Y+$script:selection.Y))
 $window.ClientSize=$script:crop.Size
 $window.BackgroundImage=New-Object Drawing.Bitmap((ExportImage));$window.BackgroundImageLayout='Zoom'
 $window.Add_KeyDown({if($_.KeyCode -eq 'Escape'){$this.Close()}})
 $window.Add_FormClosed({$this.BackgroundImage.Dispose()})
 [CaptureDpi]::DarkCaption($window.Handle,$palette.Dark)
 [void]$script:pins.Add($window);$window.Show();$form.Close()
})
function ResetSelection {
 $script:requestId++;$script:start=$null;$script:selection=[Drawing.Rectangle]::Empty
 $form.SetSelection($script:selection)
 if($script:crop){$script:crop.Dispose();$script:crop=$null}
 $script:blocks=@();ClearTranslation;$bar.Visible=$false;ShowHint '拖动重新框选';$form.Invalidate()
}
$reselect.Add_Click({ResetSelection})
$close.Add_Click({$form.Close()})
$form.Add_Shown({
 [Console]::Out.WriteLine('{"ready":true}');[Console]::Out.Flush()
})
# Populate optional OCR language choices after the first selection frame is painted.
$ocrInit=New-Object Windows.Forms.Timer
$ocrInit.Interval=100
$ocrInit.Add_Tick({
 $ocrInit.Stop()
 try {
  LoadOCR
  foreach($language in [Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages){
   [void]$source.Items.Add($language.DisplayName);$script:sourceTags+=$language.LanguageTag
  }
 } catch {Failed ('OCR 初始化失败：'+$_.Exception.Message)}
})
$ocrInit.Start()
$source.Add_SelectedIndexChanged({$script:requestId++;$script:blocks=@();ClearTranslation;Status '源语言已更改，请重新识别';Busy $false})
$target.Add_SelectedIndexChanged({$script:requestId++;ClearTranslation;Status '目标语言已更改，请重新翻译';Busy $false})
$form.Add_MouseUp({
 if($_.Button -ne 'Left'){return}
 $hadStart=$null -ne $script:start
 $script:start=$null
 if(!$hadStart -or $script:selection.Width -le 4 -or $script:selection.Height -le 4){return}
 if($script:crop){$script:crop.Dispose()}
 $script:crop=$screen.Clone($script:selection,$screen.PixelFormat)
 $script:blocks=@();ClearTranslation
 $center=New-Object Drawing.Point(($bounds.X+$script:selection.X+[int]($script:selection.Width/2)),($bounds.Y+$script:selection.Y+[int]($script:selection.Height/2)))
 $monitor=[Windows.Forms.Screen]::FromPoint($center).WorkingArea
 $script:workArea=New-Object Drawing.Rectangle(($monitor.X-$bounds.X),($monitor.Y-$bounds.Y),$monitor.Width,$monitor.Height)
 $form.SelectionScale=[CaptureDpi]::ScaleAt($center.X,$center.Y)
 $form.SetHint('','',[Drawing.Rectangle]::Empty,1)
 $bar.Arrange($form.SelectionScale,$monitor.Width)
 $bar.Visible=$true
 Status '已框选，可以翻译或提取文字'
 Busy $false
})
# The pipe is event-driven; the UI timer only marshals a completed read task onto
# the WinForms UI thread. It never sends network/status polling requests.
$timer=New-Object Windows.Forms.Timer
$timer.Interval=80
$timer.Add_Tick({
 if(!$script:readTask -or !$script:readTask.IsCompleted){return}
 $task=$script:readTask;$script:readTask=$null
 try {
  $line=$task.GetAwaiter().GetResult();if(!$line){throw '翻译服务连接已断开'}
  $reply=$line | ConvertFrom-Json
  if($reply.id -ne $script:requestId){return}
  if($reply.error){throw [string]$reply.error}
  if(@($reply.texts).Count -ne $script:blocks.Count){throw '翻译返回的区域数量不一致'}
  $script:fullTranslation=(@($reply.texts) -join "`r`n")
  $script:translated=Render @($reply.texts)
  $translate.Text='重新翻译'
  SyncOverlay
  Status '翻译完成，可以复制或钉住'
 } catch {Failed $_.Exception.Message} finally {Busy $false}
})
$timer.Start()
ShowHint '拖动框选要翻译的区域'
try {
 [void]$form.ShowDialog()
 [Console]::Out.WriteLine('{"closed":true}');[Console]::Out.Flush()
 foreach($window in $script:pins.ToArray()){if(!$window.IsDisposed){$window.Hide();[void]$window.ShowDialog()}}
} finally {
 $timer.Stop();$timer.Dispose();$ocrInit.Stop();$ocrInit.Dispose()
 if($script:crop){$script:crop.Dispose()};if($script:translated){$script:translated.Dispose()}
 foreach($window in $script:pins){if(!$window.IsDisposed){$window.Close()}}
 $screen.Dispose();$form.Dimmed.Dispose();$form.Dispose()
}
