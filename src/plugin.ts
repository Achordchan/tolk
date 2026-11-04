import { Tolk, events } from "./mygo";

export function initPluginPanel() {
  const status = document.querySelector<HTMLElement>("#plugin-status")!;
  const check = document.querySelector<HTMLButtonElement>("#plugin-check")!;
  const install = document.querySelector<HTMLButtonElement>("#plugin-install")!;
  const engine = document.querySelector<HTMLSelectElement>("#s-engine")!;
  const progress = document.querySelector<HTMLProgressElement>("#plugin-progress")!;
  const detail = document.querySelector<HTMLElement>("#plugin-detail")!;
  const mb = (n: number) => `${(n / 1048576).toFixed(1)} MB`;
  function render(s: Awaited<ReturnType<typeof Tolk.pluginStatus>>) {
    const busy = s.phase === "checking" || s.phase === "installing";
    status.textContent = s.phase === "checking" ? "正在检查插件更新…" : s.phase === "installing" ? `正在${s.stage || "检查版本"}…` :
      [s.installed ? `已安装 ${s.installed}` : "尚未安装", s.latest ? `最新版本 ${s.latest}` : "", s.message].filter(Boolean).join(" · ");
    progress.hidden = !busy;
    if (s.stage === "下载" && s.total > 0) progress.value = Math.min(100, s.done / s.total * 100);
    else progress.removeAttribute("value");
    detail.hidden = !busy;
    detail.textContent = [s.latest ? `版本 ${s.latest}` : "", s.route, s.stage === "下载" ? (s.total > 0 ? `${Math.floor(s.done / s.total * 100)}% · ${mb(s.done)} / ${mb(s.total)}` : `已下载 ${mb(s.done)}`) : ""].filter(Boolean).join(" · ");
    check.disabled = busy;
    check.hidden = busy;
    install.disabled = busy;
    install.hidden = busy || s.phase !== "error" && !!s.installed && (!s.latest || s.latest === s.installed);
    install.textContent = s.phase === "error" ? "重试安装" : s.installed ? "更新插件" : "安装插件";
  }
  async function checkNow() {
    try { render(await Tolk.checkPlugin()); }
    catch (e) { status.textContent = `检查失败：${String(e)}`; check.disabled = false; install.disabled = false; }
  }
  check.addEventListener("click", () => void checkNow());
  install.addEventListener("click", async () => {
    install.disabled = check.disabled = true;
    try { await Tolk.installPlugin(); }
    catch (e) { status.textContent = `安装失败：${String(e)}`; install.disabled = check.disabled = false; }
  });
  events.pluginChanged.on(render);
  return async () => {
    if (engine.value !== "achord_builtin") return;
    try {
      const s = await Tolk.pluginStatus();
      render(s);
      if (s.phase !== "error" && s.phase !== "installing" && s.phase !== "checking" && (!s.installed || (s.latest && s.latest !== s.installed))) {
        await Tolk.installPlugin();
      }
    }
    catch (e) { status.textContent = String(e); }
  };
}
