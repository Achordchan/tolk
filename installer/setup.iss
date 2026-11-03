; Tolk 的 Windows 安装器。AppId 沿用 2.2.2 及以前的，所以覆盖安装时系统里的
; 卸载项还是同一个。2.2.3 起主程序叫 Tolk.exe、装在 Tolk 文件夹；从旧版升级时
; 先关掉旧程序，装完删掉旧文件夹（旧版装在「大佐翻译官」文件夹，主程序同名）。
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

// 旧版装在哪里：同一个 AppId 的卸载项里记着。
function InitializeSetup(): Boolean;
begin
  if not RegQueryStringValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{5E76B529-0B3B-4E93-AE4D-DCF0EA7174CE}_is1', 'InstallLocation', OldDir) then
    OldDir := '';
  OldDir := RemoveBackslashUnlessRoot(OldDir);
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

// 装好后删掉旧文件夹：只在它确实是旧版目录（里面有旧主程序）且不是这次的目录时。
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and (OldDir <> '') and
     (CompareText(OldDir, RemoveBackslashUnlessRoot(ExpandConstant('{app}'))) <> 0) and
     FileExists(AddBackslash(OldDir) + '{#OldExeName}') then
    DelTree(OldDir, True, True, True);
end;
