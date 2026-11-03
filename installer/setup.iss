; Tolk 的 Windows 安装器。AppId 沿用 2.2.2 及以前的，所以覆盖安装时系统里的
; 卸载项还是同一个。2.2.3 起主程序叫 Tolk.exe、装在 Tolk 文件夹；从旧版升级时
; 先关掉旧程序，装完删掉旧版的文件（旧版装在「大佐翻译官」文件夹，主程序同名）；
; 文件夹里别的东西不动，空了才删文件夹。
#ifndef MyAppVersion
  #error 需要 /DMyAppVersion=x.y.z
#endif
#ifndef SourceExe
  #error 需要 /DSourceExe=主程序路径
#endif
#ifndef IconFile
  #define IconFile "..\design\logo.ico"
#endif
#define MyAppName "Tolk"
#define MyAppExeName "Tolk.exe"
#define OldExeName "大佐翻译官.exe"
#define OldLnkName "大佐翻译官.lnk"
#define ApprovedKey "Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\StartupFolder"

[Setup]
AppId={{5E76B529-0B3B-4E93-AE4D-DCF0EA7174CE}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
VersionInfoVersion={#MyAppVersion}
AppPublisher=Achord
AppPublisherURL=https://github.com/Achordchan/tolk
DefaultDirName={code:InstallDir}
DefaultGroupName={#MyAppName}
PrivilegesRequired=lowest
; 上次的目录由 InstallDir 决定：旧版的「大佐翻译官」目录不沿用。
UsePreviousAppDir=no
OutputDir=..\dist
OutputBaseFilename=Tolk-setup-windows_{#MyAppVersion}
Compression=lzma
SolidCompression=yes
SetupIconFile={#IconFile}
UninstallDisplayIcon={app}\{#MyAppExeName}
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[InstallDelete]
; 选了和旧版同一个目录时，旧名字的主程序。
Type: files; Name: "{app}\{#OldExeName}"

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "{#MyAppExeName}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#MyAppName}}"; Flags: nowait postinstall skipifsilent

[Code]
var
  OldDir: String;
  OldUninstaller: String;

// 旧版装在哪里：同一个 AppId 的卸载项里记着。
function InitializeSetup(): Boolean;
begin
  if not RegQueryStringValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{5E76B529-0B3B-4E93-AE4D-DCF0EA7174CE}_is1', 'InstallLocation', OldDir) then
    OldDir := '';
  OldDir := RemoveBackslashUnlessRoot(OldDir);
  // 旧版自己的卸载程序（目录里可能还有别的程序的 unins000.exe）。
  if not RegQueryStringValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{5E76B529-0B3B-4E93-AE4D-DCF0EA7174CE}_is1', 'UninstallString', OldUninstaller) then
    OldUninstaller := '';
  OldUninstaller := RemoveQuotes(OldUninstaller);
  Result := True;
end;

// 2.2.3 及以后装过的就装回原处；2.2.2 及以前的「大佐翻译官」目录换成 Tolk 目录。
function InstallDir(Param: String): String;
begin
  if (OldDir <> '') and not FileExists(AddBackslash(OldDir) + '{#OldExeName}') then
    Result := OldDir
  else
    Result := ExpandConstant('{localappdata}\Programs\Tolk');
end;

// 旧版主程序换了名字，安装器的“关闭正在运行的程序”认不出它，所以直接结束它。
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM "{#OldExeName}"', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Result := '';
end;

// 旧版目录里属于旧版的文件：主程序、更新包带来的文件、旧的卸载程序。
procedure DeleteOldFiles(Dir: String);
begin
  Dir := AddBackslash(Dir);
  DeleteFile(Dir + '{#OldExeName}');
  DeleteFile(Dir + 'update_manifest.json');
  DeleteFile(Dir + 'LICENSE');
  DeleteFile(Dir + 'DeepLX-LICENSE');
  DeleteFile(Dir + 'src\ziyuan\logo.ico');
  // 只删旧版自己的卸载程序：卸载项里记着的那个，且就在这个目录里。
  if (OldUninstaller <> '') and
     (CompareText(AddBackslash(ExtractFileDir(OldUninstaller)), Dir) = 0) then
  begin
    DeleteFile(OldUninstaller);
    DeleteFile(ChangeFileExt(OldUninstaller, '.dat'));
    DeleteFile(ChangeFileExt(OldUninstaller, '.msg'));
  end;
  // 只删空了的文件夹：用户放进去的别的东西都留着。
  RemoveDir(Dir + 'src\ziyuan');
  RemoveDir(Dir + 'src');
  RemoveDir(RemoveBackslashUnlessRoot(Dir));
end;

// 开机自启：2.2.2 及以前的快捷方式叫「大佐翻译官」、指向旧主程序，旧主程序一删
// 它就失效了。不等新版打开再修（静默安装、没勾“运行”时不会打开），装的时候就
// 换成指向新主程序的 Tolk 快捷方式，「启动应用」里的开关状态一起带过去。
// Tolk 快捷方式本来就在的，也指到这次装的位置。
procedure MoveStartupShortcut;
var
  OldLnk, NewLnk: String;
  Approved: AnsiString;
begin
  OldLnk := ExpandConstant('{userstartup}\{#OldLnkName}');
  NewLnk := ExpandConstant('{userstartup}\Tolk.lnk');
  if not FileExists(OldLnk) and not FileExists(NewLnk) then
    Exit;
  if FileExists(OldLnk) and not FileExists(NewLnk) and
     RegQueryBinaryValue(HKCU, '{#ApprovedKey}', '{#OldLnkName}', Approved) then
    RegWriteBinaryValue(HKCU, '{#ApprovedKey}', 'Tolk.lnk', Approved);
  try
    CreateShellLink(NewLnk, 'Tolk', ExpandConstant('{app}\{#MyAppExeName}'), '',
      ExpandConstant('{app}'), ExpandConstant('{app}\{#MyAppExeName}'), 0, SW_SHOWNORMAL);
  except
    Log('开机自启快捷方式没建成：' + GetExceptionMessage);
    Exit;
  end;
  if FileExists(OldLnk) then
  begin
    DeleteFile(OldLnk);
    RegDeleteValue(HKCU, '{#ApprovedKey}', '{#OldLnkName}');
  end;
end;

// 桌面快捷方式：旧版装的 Tolk 快捷方式指向旧主程序。这次没勾“创建桌面快捷方式”
// 时安装器不会碰它，旧主程序一删它就打不开了，所以只要它在就指到新主程序。
procedure RetargetDesktopShortcut;
var
  Lnk: String;
begin
  Lnk := ExpandConstant('{autodesktop}\{#MyAppName}.lnk');
  if not FileExists(Lnk) then
    Exit;
  try
    CreateShellLink(Lnk, '{#MyAppName}', ExpandConstant('{app}\{#MyAppExeName}'), '',
      ExpandConstant('{app}'), ExpandConstant('{app}\{#MyAppExeName}'), 0, SW_SHOWNORMAL);
  except
    Log('桌面快捷方式没改成：' + GetExceptionMessage);
  end;
end;

// 装好后先修开机自启，再清掉旧版：只在旧目录确实是旧版的（里面有旧主程序）且不是这次的目录时。
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    MoveStartupShortcut;
    RetargetDesktopShortcut;
  end;
  if (CurStep = ssPostInstall) and (OldDir <> '') and
     (CompareText(OldDir, RemoveBackslashUnlessRoot(ExpandConstant('{app}'))) <> 0) and
     FileExists(AddBackslash(OldDir) + '{#OldExeName}') then
    DeleteOldFiles(OldDir);
end;
