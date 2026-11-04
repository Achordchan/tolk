# Merge wrapped lines conservatively: never join neighboring columns or list items.
function GroupParagraphs($lines) {
 $groups=New-Object System.Collections.Generic.List[object]
 foreach($line in $lines) {
  $previous=if($groups.Count){$groups[$groups.Count-1]}else{$null}
  $merge=$false
  if($previous) {
   $gap=$line.Y-($previous.Y+$previous.H)
   $height=$previous.LineHeight
   $merge=$gap -ge -2 -and $gap -le $height*0.8 -and [Math]::Abs($line.X-$previous.X) -le $height*0.6 -and $line.H/$height -gt 0.65 -and $line.H/$height -lt 1.5 -and $previous.LastWidth -ge $line.W*0.8 -and $previous.Text -notmatch '[。！？.!?:：;；]$' -and $line.Text -notmatch '^\s*([•\-]|\d+[.)、])'
  }
  if($merge) {
   $separator=if($previous.Text -match '[\p{IsCJKUnifiedIdeographs}]$' -and $line.Text -match '^[\p{IsCJKUnifiedIdeographs}]'){''}else{' '}
   $previous.Text+=$separator+$line.Text
   $previous.W=[Math]::Max($previous.W,$line.X+$line.W-$previous.X)
   $previous.H=$line.Y+$line.H-$previous.Y
   $previous.LastWidth=$line.W
  } else {
   $groups.Add([PSCustomObject]@{Text=$line.Text;X=$line.X;Y=$line.Y;W=$line.W;H=$line.H;LineHeight=$line.H;LastWidth=$line.W})
  }
 }
 return $groups.ToArray()
}
