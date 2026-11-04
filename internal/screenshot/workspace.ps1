# Loaded after capture.ps1 initializes the frozen screen, selection form and OCR bridge.
$script:crop = $null
$script:translated = $null
$script:blocks = @()
$script:requestId = 0
$script:readTask = $null
$script:pins = New-Object System.Collections.Generic.List[System.Windows.Forms.Form]
$script:fullTranslation = ''
$script:working = $false
$bar = New-Object System.Windows.Forms.FlowLayoutPanel
$bar.AutoSize = $true
$bar.WrapContents = $true
$bar.MaximumSize = New-Object Drawing.Size(([Math]::Min(900,$bounds.Width)),0)
$bar.Padding = New-Object System.Windows.Forms.Padding(8)
$bar.BackColor = [Drawing.Color]::FromArgb(245,245,245)
$bar.Visible = $false
$form.Controls.Add($bar)
function Button($label, $action) {
 $button = New-Object System.Windows.Forms.Button
 $button.Text=$label; $button.AutoSize=$true; $button.MinimumSize=New-Object Drawing.Size(78,36)
 $button.FlatStyle='Flat';$button.FlatAppearance.BorderSize=0
 $button.Padding=New-Object Windows.Forms.Padding(10,5,10,5)
 $button.Margin=New-Object Windows.Forms.Padding(4)
 $button.Cursor=[Windows.Forms.Cursors]::Hand
 $button.Add_Click($action)
 [void]$bar.Controls.Add($button)
 return $button
}
$source = New-Object System.Windows.Forms.ComboBox
$source.DropDownStyle='DropDownList';$source.Width=150
[void]$source.Items.Add('自动检测')
$script:sourceTags=@('')
$source.SelectedIndex=0
$source.FlatStyle='Flat';$source.Margin=New-Object Windows.Forms.Padding(4,8,8,4)
[void]$bar.Controls.Add($source)
$target = New-Object System.Windows.Forms.ComboBox
$target.DropDownStyle='DropDownList';$target.Width=130
foreach ($name in ($env:TOLK_SHOT_LANGUAGES | ConvertFrom-Json)) { [void]$target.Items.Add([string]$name) }
$target.FlatStyle='Flat';$target.Margin=New-Object Windows.Forms.Padding(4,8,8,4)
$target.SelectedItem=$env:TOLK_SHOT_TARGET
if ($target.SelectedIndex -lt 0) { $target.SelectedIndex=0 }
[void]$bar.Controls.Add($target)
$hint = New-Object System.Windows.Forms.Label
$hint.AutoSize=$false;$hint.Height=60;$hint.Width=600
$hint.BackColor=[Drawing.Color]::FromArgb(245,245,245)
$hint.ForeColor=[Drawing.Color]::FromArgb(40,40,40)
$hint.Padding=New-Object System.Windows.Forms.Padding(8)
$hint.Text='拖动框选区域；Esc 退出。截图和识别在本机完成，仅识别文字发送给当前翻译服务。'
$hint.Location=New-Object Drawing.Point(20,20)
$form.Controls.Add($hint)
function Status($text) { $hint.Text=$text }
function Busy($on) {
 $script:working=$on
 $translate.Enabled=!$on;$extract.Enabled=!$on;$finish.Enabled=!$on;$pin.Enabled=!$on
 $source.Enabled=!$on;$target.Enabled=!$on
 if ($reselect) {$reselect.Enabled=!$on}
 $compare.Enabled=(!$on -and $null -ne $script:translated)
}
function ClearTranslation {
 $script:fullTranslation=''
 if($viewText){$viewText.Enabled=$false}
 if ($script:translated) {$script:translated.Dispose();$script:translated=$null}
 $compare.Checked=$false;$compare.Enabled=$false
 $form.Invalidate()
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
$compare=New-Object Windows.Forms.CheckBox
$compare.Text='原文对照';$compare.AutoSize=$true;$compare.Enabled=$false
$compare.Margin=New-Object Windows.Forms.Padding(8,10,8,4)
$compare.Add_CheckedChanged({$form.Invalidate()})
[void]$bar.Controls.Add($compare)
$translate=Button '翻译' {
 try {
  Busy $true;Status '正在识别文字…';$form.Refresh()
  Recognize
  ClearTranslation
  $script:requestId++
  # OCR language selection controls recognition; translation auto-detects the recognized text.
  $request=@{id=$script:requestId;source='自动检测';target=[string]$target.SelectedItem;blocks=@($script:blocks | ForEach-Object {$_.Text})}
  $json=$request | ConvertTo-Json -Compress -Depth 5
  [Console]::Out.WriteLine($json);[Console]::Out.Flush()
  $script:readTask=[CaptureDpi]::ReadReply()
  Status ('正在翻译 '+$script:blocks.Count+' 个文字区域…')
 } catch {Status $_.Exception.Message;Busy $false}
}
$extract=Button '提取原文' {
 try{Busy $true;Status '正在识别文字…';$form.Refresh();Recognize;[Windows.Forms.Clipboard]::SetText(($script:blocks | ForEach-Object {$_.Text}) -join "`r`n");Status '原文已复制'}catch{Status $_.Exception.Message}finally{Busy $false}
}
function ExportImage {if($script:translated -and !$compare.Checked){return $script:translated};return $script:crop}
$viewText=Button '查看译文' {
 $textWindow=New-Object Windows.Forms.Form
 $textWindow.Text='完整译文';$textWindow.Size=New-Object Drawing.Size(560,400);$textWindow.StartPosition='CenterParent'
 $textBox=New-Object Windows.Forms.TextBox
 $textBox.Multiline=$true;$textBox.ReadOnly=$true;$textBox.ScrollBars='Vertical';$textBox.Dock='Fill';$textBox.Text=$script:fullTranslation
 $textBox.Font=New-Object Drawing.Font('Microsoft YaHei',11)
 $textWindow.Controls.Add($textBox)
 try{[void]$textWindow.ShowDialog($form)}finally{$textBox.Font.Dispose();$textWindow.Dispose()}
}
$viewText.Enabled=$false
$finish=Button '复制完成'  {
 try {[Windows.Forms.Clipboard]::SetImage((ExportImage));$form.Close()}catch{Status ('复制失败：'+$_.Exception.Message)}
}
$pin=Button '钉到屏幕' {
 $window=New-Object Windows.Forms.Form
 $window.Text='Tolk 截图 · Esc 关闭';$window.TopMost=$true;$window.KeyPreview=$true
 $window.StartPosition='Manual';$window.Location=New-Object Drawing.Point(($bounds.X+$script:selection.X),($bounds.Y+$script:selection.Y))
 $window.ClientSize=$script:crop.Size
 $window.BackgroundImage=New-Object Drawing.Bitmap((ExportImage));$window.BackgroundImageLayout='Zoom'
 $window.Add_KeyDown({if($_.KeyCode -eq 'Escape'){$this.Close()}})
 $window.Add_FormClosed({$this.BackgroundImage.Dispose()})
 [void]$script:pins.Add($window);$window.Show();$form.Close()
}
function ResetSelection {
 $script:requestId++;$script:start=$null;$script:selection=[Drawing.Rectangle]::Empty
 if($script:crop){$script:crop.Dispose();$script:crop=$null}
 $script:blocks=@();ClearTranslation;$bar.Visible=$false;Status '拖动重新框选；Esc 退出';$form.Invalidate()
}
$reselect=Button '重新框选' { ResetSelection }
$close=Button '退出' {$form.Close()}
$dark=$env:TOLK_SHOT_DARK -eq '1'
$surface=if($dark){[Drawing.Color]::FromArgb(36,38,42)}else{[Drawing.Color]::FromArgb(249,250,252)}
$controlColor=if($dark){[Drawing.Color]::FromArgb(54,57,63)}else{[Drawing.Color]::FromArgb(232,236,242)}
$foreground=if($dark){[Drawing.Color]::FromArgb(235,237,241)}else{[Drawing.Color]::FromArgb(35,39,47)}
$bar.BackColor=$surface;$hint.BackColor=$surface;$hint.ForeColor=$foreground
foreach($control in $bar.Controls){$control.BackColor=$controlColor;$control.ForeColor=$foreground}
$translate.BackColor=[Drawing.Color]::FromArgb(10,132,255);$translate.ForeColor=[Drawing.Color]::White
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
 } catch {Status ('OCR 初始化失败：'+$_.Exception.Message)}
})
$ocrInit.Start()
$source.Add_SelectedIndexChanged({$script:requestId++;$script:blocks=@();ClearTranslation;Status '源语言已更改，请重新识别';Busy $false})
$target.Add_SelectedIndexChanged({$script:requestId++;ClearTranslation;Status '目标语言已更改，请重新翻译';Busy $false})
$form.Add_Paint({
 if($script:crop -and $script:translated -and !$compare.Checked){$_.Graphics.DrawImage($script:translated,$script:selection)}
})
$form.Add_MouseUp({
 if(!$script:start -or $_.Button -ne 'Left' -or $script:selection.Width -le 4 -or $script:selection.Height -le 4){return}
 $script:start=$null
 if($script:crop){$script:crop.Dispose()}
 $script:crop=$screen.Clone($script:selection,$screen.PixelFormat)
 $script:blocks=@();ClearTranslation
 $bar.Visible=$true;$bar.PerformLayout()
 $x=[Math]::Max(0,[Math]::Min($script:selection.X,$form.ClientSize.Width-$bar.Width))
 $y=$script:selection.Bottom+8
 if($y+$bar.Height+70 -gt $form.ClientSize.Height){$y=[Math]::Max(0,$script:selection.Top-$bar.Height-70)}
 $bar.Location=New-Object Drawing.Point($x,$y)
 $hint.Width=[Math]::Min($bar.Width,$form.ClientSize.Width)
 $hint.Location=New-Object Drawing.Point($x,([Math]::Min($form.ClientSize.Height-$hint.Height,$y+$bar.Height+4)))
 Status '已框选。点击翻译回贴译文，或提取文字、复制原图、钉到屏幕。'
 Busy $false;$form.Invalidate()
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
  $viewText.Enabled=$true
  $script:translated=Render @($reply.texts)
  Status '翻译完成。可切换原文对照，或复制、钉住当前图片。'
  $form.Invalidate()
 } catch {Status $_.Exception.Message} finally {Busy $false}
})
$timer.Start()
try {
 [void]$form.ShowDialog()
 [Console]::Out.WriteLine('{"closed":true}');[Console]::Out.Flush()
 foreach($window in $script:pins.ToArray()){if(!$window.IsDisposed){$window.Hide();[void]$window.ShowDialog()}}
} finally {
 $timer.Stop();$timer.Dispose();$ocrInit.Stop();$ocrInit.Dispose()
 if($script:crop){$script:crop.Dispose()};if($script:translated){$script:translated.Dispose()}
 foreach($window in $script:pins){if(!$window.IsDisposed){$window.Close()}}
 $screen.Dispose();$form.Dispose()
}
