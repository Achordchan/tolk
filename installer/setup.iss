; Tolk 的 Windows 安装器。AppId、安装目录和主程序文件名沿用大佐翻译官，
; 这样老用户直接运行它就是原地升级：设置、历史、开机自启快捷方式都不用动。
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
#define MyAppExeName "大佐翻译官.exe"

[Setup]
AppId={{5E76B529-0B3B-4E93-AE4D-DCF0EA7174CE}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
VersionInfoVersion={#MyAppVersion}
AppPublisher=Achord
AppPublisherURL=https://github.com/Achordchan/tolk
DefaultDirName={localappdata}\Programs\大佐翻译官
DefaultGroupName={#MyAppName}
PrivilegesRequired=lowest
UsePreviousAppDir=yes
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

; 老版本（Python / Nuitka）留下的运行库，换成新程序后用不上了。只删这些已知的名字，不碰安装目录里的其他东西。
[InstallDelete]
Type: files; Name: "{app}\*.dll"
Type: files; Name: "{app}\*.pyd"
Type: filesandordirs; Name: "{app}\PyQt5"
Type: filesandordirs; Name: "{app}\qtawesome"
Type: filesandordirs; Name: "{app}\engines"
Type: filesandordirs; Name: "{app}\src"
Type: filesandordirs; Name: "{app}\certifi"

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "{#MyAppExeName}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#MyAppName}}"; Flags: nowait postinstall skipifsilent
