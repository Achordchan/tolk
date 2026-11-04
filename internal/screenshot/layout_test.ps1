$ErrorActionPreference='Stop'
. "$PSScriptRoot/layout.ps1"
function Line($text,$x,$y,$w=100) { [PSCustomObject]@{Text=$text;X=$x;Y=$y;W=$w;H=20} }
$group=@(GroupParagraphs @((Line 'This is a wrapped' 0 0),(Line 'sentence.' 0 24)))
if($group.Count -ne 1 -or $group[0].Text -ne 'This is a wrapped sentence.' -or $group[0].H -ne 44){throw 'wrapped paragraph not merged'}
$group=@(GroupParagraphs @((Line 'Left column' 0 0),(Line 'Right column' 200 24)))
if($group.Count -ne 2){throw 'columns merged'}
$group=@(GroupParagraphs @((Line 'Finished.' 0 0),(Line 'New sentence' 0 24)))
if($group.Count -ne 2){throw 'sentence boundary merged'}
'Paragraph layout tests passed'
