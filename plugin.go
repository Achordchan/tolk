package main

import (
	"github.com/egoist/mygo"
	"path/filepath"
	"runtime"
	"tolk/internal/dlx"
	"tolk/internal/legacy"
	"tolk/internal/update"
)

var PluginChanged = mygo.NewEvent[dlx.State]("pluginChanged")

func initPlugin() {
	checker := update.NewChecker(runtime.GOOS, "plugin")
	checker.Logf = Logs.Infof
	dlx.Download = checker.DownloadArtifact
	home, err := legacy.Home()
	if err != nil {
		Logs.Errorf("插件目录不可用：%v", err)
		return
	}
	dlx.Configure(filepath.Join(home, "plugins", "dlx"), func(s dlx.State) { _ = PluginChanged.Broadcast(s) })
}
func (Tolk) PluginStatus() dlx.State { return dlx.Status() }
func (Tolk) CheckPlugin() dlx.State  { return dlx.Check() }
func (Tolk) InstallPlugin()          { dlx.Install() }
