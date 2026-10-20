import { Tolk } from "./mygo";
import type { MacPane } from "./settings";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

type Status = Awaited<ReturnType<typeof Tolk.macStatus>>;
type IaConfig = Awaited<ReturnType<typeof Tolk.getInputAssist>>["settings"];

const MAX_TARGETS = 6;
const RECOMMENDED_TARGETS = 3;
// Carbon modifier bits, as the old app's carbonModifiers.
const CMD = 256;
const SHIFT = 512;
const OPTION = 2048;
const CONTROL = 4096;
const KEY_NAMES: Record<number, string> = { 49: "Space", 36: "Return", 0: "A", 2: "D", 14: "E", 17: "T", 5: "G", 38: "J" };

/** The old app's InputAssistShortcut.selectableOptions, default first. */
const SHORTCUTS: [number, number][] = [
  [49, OPTION | SHIFT],
  [49, OPTION],
  [49, CONTROL],
  [17, OPTION],
  [5, OPTION],
  [38, CMD | OPTION],
];

function shortcutLabel(key: number, mods: number) {
  return (
    (mods & CONTROL ? "⌃" : "") + (mods & OPTION ? "⌥" : "") + (mods & SHIFT ? "⇧" : "") + (mods & CMD ? "⌘" : "") + (KEY_NAMES[key] ?? `Key ${key}`)
  );
}

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

  // ---- 输入助手 ----
  const iaEnabled = $<HTMLInputElement>("#ia-enabled");
  const iaBody = $("#ia-body");
  const iaShortcut = $<HTMLSelectElement>("#ia-shortcut");
  const iaTargets = $("#ia-targets");
  const iaCount = $("#ia-count");
  const iaScope = $<HTMLSelectElement>("#ia-scope");
  const iaBlock = $<HTMLTextAreaElement>("#ia-block");
  const iaAllow = $<HTMLTextAreaElement>("#ia-allow");
  const iaAuto = $<HTMLInputElement>("#ia-auto");
  const iaBadge = $<HTMLInputElement>("#ia-badge");
  const iaTest = $<HTMLButtonElement>("#ia-test");
  const iaStatus = $("#ia-status");
  let iaLoaded = false;
  let iaSavedEnabled = false;
  let iaTargetList: string[] = [];
  let iaLangs: string[] = [];

  function drawTargets() {
    const full = iaTargetList.length >= MAX_TARGETS;
    iaTargets.replaceChildren(
      ...iaLangs.map((name) => {
        const on = iaTargetList.includes(name);
        const label = document.createElement("label");
        const box = document.createElement("input");
        box.type = "checkbox";
        box.checked = on;
        box.disabled = on ? iaTargetList.length <= 1 : full;
        label.classList.toggle("off", box.disabled && !on);
        box.addEventListener("change", () => {
          iaTargetList = box.checked ? [...iaTargetList, name] : iaTargetList.filter((x) => x !== name);
          drawTargets();
        });
        label.append(box, document.createTextNode(name));
        return label;
      }),
    );
    iaCount.textContent =
      `已选 ${iaTargetList.length} 个，最多 ${MAX_TARGETS} 个，按勾选的先后顺序展示，默认高亮第一项。` +
      (iaTargetList.length > RECOMMENDED_TARGETS ? `候选语言较多可能降低选择效率，建议不超过 ${RECOMMENDED_TARGETS} 个。` : "");
  }

  function drawScope() {
    const allow = iaScope.value === "allowlist";
    $("#ia-list-title").textContent = "黑名单";
    $("#ia-allow-row").hidden = !allow;
    $("#ia-list-hint").textContent = allow
      ? "每行一个，可填应用名或 Bundle ID。黑名单优先于「仅在这些应用启用」；密码框和安全输入状态永远跳过。"
      : "每行一个，可填应用名或 Bundle ID。密码框和安全输入状态永远跳过。";
  }

  function drawShortcuts(key: number, mods: number) {
    const opts = SHORTCUTS.slice();
    if (!opts.some(([k, m]) => k === key && m === mods)) opts.push([key, mods]); // an imported custom one stays selectable
    iaShortcut.replaceChildren(...opts.map(([k, m]) => new Option(shortcutLabel(k, m), `${k}-${m}`)));
    iaShortcut.value = `${key}-${mods}`;
  }

  function drawIaStatus(st: Status) {
    iaStatus.className = "muted ia-status";
    let text = "";
    if (!st.running) text = "原生助手未运行";
    else if (iaSavedEnabled) {
      if (st.inputAssistActive) {
        text = "输入助手已启动";
        iaStatus.classList.add("ok");
      } else {
        text = st.inputAssistError || "输入助手未启动";
        iaStatus.classList.add("no");
      }
    }
    iaStatus.textContent = text;
    iaTest.toggleAttribute("disabled", !(st.running && iaSavedEnabled && st.inputAssistActive));
  }

  iaEnabled.addEventListener("change", () => (iaBody.hidden = !iaEnabled.checked));
  iaScope.addEventListener("change", drawScope);
  iaTest.addEventListener("click", async () => {
    try {
      await Tolk.inputAssistTrigger();
    } catch (e) {
      toast(String(e instanceof Error ? e.message : e));
    }
  });

  async function refresh() {
    const st: Status = await Tolk.macStatus();
    badge(ax, st.accessibility, st.running);
    badge(sr, st.screenRecording, st.running);
    $("#p-guide").toggleAttribute("disabled", !st.running);
    $("#p-shot").toggleAttribute("disabled", !st.running);
    $("#p-migrate").toggleAttribute("disabled", !st.running);
    drawIaStatus(st);
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
      try {
        iaLangs = await Tolk.languages();
        const ia = await Tolk.getInputAssist();
        const c = ia.settings;
        iaLoaded = true;
        iaSavedEnabled = c.enabled;
        iaEnabled.checked = c.enabled;
        iaBody.hidden = !c.enabled;
        drawShortcuts(c.shortcutKeyCode, c.shortcutModifiers);
        iaTargetList = c.targets.slice();
        drawTargets();
        iaScope.value = c.scope;
        iaBlock.value = c.blocklist;
        iaAllow.value = c.allowlist;
        iaAuto.checked = c.autoShow;
        iaBadge.checked = c.cacheBadge;
        drawScope();
      } catch (e) {
        iaLoaded = false; // do not save a form that never loaded
        toast(String(e instanceof Error ? e.message : e));
      }
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
      if (iaLoaded) {
        const [key, mods] = iaShortcut.value.split("-").map(Number) as [number, number];
        const next: IaConfig = {
          enabled: iaEnabled.checked,
          targets: iaTargetList,
          shortcutKeyCode: key,
          shortcutModifiers: mods,
          scope: iaScope.value,
          blocklist: iaBlock.value,
          allowlist: iaAllow.value,
          autoShow: iaAuto.checked,
          cacheBadge: iaBadge.checked,
        };
        const st = await Tolk.setInputAssist(next);
        iaSavedEnabled = st.settings.enabled;
        if (st.settings.enabled && !st.active && st.error) toast(`输入助手：${st.error}`);
      }
    },
  };
}
