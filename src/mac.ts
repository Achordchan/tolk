import { Tolk } from "./mygo";
import type { MacPane } from "./settings";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

type Status = Awaited<ReturnType<typeof Tolk.macStatus>>;

function badge(el: HTMLElement, on: boolean, running: boolean) {
  el.className = "badge " + (!running ? "" : on ? "ok" : "no");
  el.textContent = !running ? "助手未运行" : on ? "已授权" : "未授权";
}

/** The macOS-only part of the settings sheet: hotkeys, permissions, screenshot, import. */
export function macPane(toast: (m: string) => void, close: () => void): MacPane {
  const system = $<HTMLInputElement>("#s-hk-dc");
  const shot = $<HTMLInputElement>("#s-hk-shot");
  const ax = $("#p-ax");
  const sr = $("#p-sr");
  let hkNote: HTMLElement | null = null;
  let timer: number | undefined;

  async function refresh() {
    const st: Status = await Tolk.macStatus();
    badge(ax, st.accessibility, st.running);
    badge(sr, st.screenRecording, st.running);
    $("#p-guide").toggleAttribute("disabled", !st.running);
    $("#p-shot").toggleAttribute("disabled", !st.running);
    $("#p-migrate").toggleAttribute("disabled", !st.running);
    if (hkNote) {
      hkNote.textContent = st.hotkeyError ? `系统级快捷键未启动：${st.hotkeyError}` : st.hotkeyRunning ? "系统级快捷键正在运行" : "";
    }
  }

  const act = (id: string, fn: () => Promise<unknown>, done?: string) =>
    $(id).addEventListener("click", async () => {
      try {
        await fn();
        if (done) toast(done);
        await refresh();
      } catch (e) {
        toast(String(e instanceof Error ? e.message : e));
      }
    });

  act("#p-guide", () => Tolk.macGuide());
  act("#p-shot", async () => {
    close(); // get the sheet out of the way before the selection layer opens
    await Tolk.macScreenshot("translate");
  });
  act("#p-migrate", async () => {
    const notes = await Tolk.macImportLegacy();
    toast(notes.length ? notes[0]! : "已导入旧版设置，请重新打开设置查看");
  });
  ax.addEventListener("click", () => void Tolk.macRequestPermission("accessibility").catch((e) => toast(String(e))));
  sr.addEventListener("click", () => void Tolk.macRequestPermission("screenRecording").catch((e) => toast(String(e))));

  return {
    async load() {
      const p = await Tolk.getPrefs();
      system.checked = p.systemHotkey;
      shot.checked = p.screenshotHotkey;
      if (!hkNote) {
        hkNote = document.createElement("p");
        hkNote.className = "muted";
        hkNote.id = "p-note";
        shot.closest("fieldset")!.append(hkNote);
      }
      await refresh();
      window.clearInterval(timer);
      timer = window.setInterval(() => {
        if (!$("#settings").hasAttribute("open")) return window.clearInterval(timer);
        void refresh().catch(() => {});
      }, 2000); // grants show up without reopening
    },
    async save() {
      window.clearInterval(timer);
      const p = await Tolk.getPrefs();
      await Tolk.setMacHotkeys(system.checked, shot.checked, p.screenshotKeyCode);
    },
  };
}
