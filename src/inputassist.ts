import { Tolk } from "./mygo";
import type { MacPane } from "./settings";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

type IaConfig = Awaited<ReturnType<typeof Tolk.getInputAssist>>["settings"];

const MAX_TARGETS = 6;
const RECOMMENDED_TARGETS = 3;
// Carbon modifier bits, as the old app's carbonModifiers. Windows reads them as
// ⌃ Ctrl, ⌥ Alt, ⇧ Shift, ⌘ Win.
const CMD = 256;
const SHIFT = 512;
const OPTION = 2048;
const CONTROL = 4096;
const KEY_NAMES: Record<number, string> = { 49: "Space", 36: "Return", 0: "A", 2: "D", 14: "E", 17: "T", 5: "G", 38: "J" };

/** The old app's InputAssistShortcut.selectableOptions, default first. */
const MAC_SHORTCUTS: [number, number][] = [
  [49, OPTION | SHIFT],
  [49, OPTION],
  [49, CONTROL],
  [17, OPTION],
  [5, OPTION],
  [38, CMD | OPTION],
];

/** Windows: Alt+letter would take the menu shortcuts of every app, so letters go with Ctrl+Alt. */
const WIN_SHORTCUTS: [number, number][] = [
  [49, OPTION | SHIFT],
  [49, CONTROL | OPTION],
  [17, CONTROL | OPTION],
  [5, CONTROL | OPTION],
  [38, CONTROL | OPTION],
];

/** 输入助手's tab of the settings sheet, on macOS and Windows. */
export function inputAssistPane(os: string, toast: (m: string) => void, close: () => void): MacPane {
  const win = os === "windows";
  const SHORTCUTS = win ? WIN_SHORTCUTS : MAC_SHORTCUTS;

  const enabled = $<HTMLInputElement>("#ia-enabled");
  const body = $("#ia-body");
  const shortcut = $<HTMLSelectElement>("#ia-shortcut");
  const targets = $("#ia-targets");
  const count = $("#ia-count");
  const scope = $<HTMLSelectElement>("#ia-scope");
  const block = $<HTMLTextAreaElement>("#ia-block");
  const allow = $<HTMLTextAreaElement>("#ia-allow");
  const auto = $<HTMLInputElement>("#ia-auto");
  const badge = $<HTMLInputElement>("#ia-badge");
  const test = $<HTMLButtonElement>("#ia-test");
  const status = $("#ia-status");
  let loaded = false;
  let savedEnabled = false;
  let targetList: string[] = [];
  let langs: string[] = [];
  let timer: number | undefined;
  // Apple 本地翻译 needs a language pack per target: ask for it when the
  // language is picked, not later in the middle of typing.
  let apple = false;
  let packs: Record<string, string> = {};
  const downloading = new Set<string>();

  async function checkPacks(names: string[]) {
    if (!apple || !names.length) return;
    try {
      Object.assign(packs, await Tolk.appleLanguagePacks(names));
    } catch {
      /* the helper may be restarting */
    }
    drawTargets();
  }

  async function download(name: string) {
    if (downloading.has(name)) return;
    downloading.add(name);
    drawTargets();
    toast(`正在准备「${name}」语言包，请在系统弹出的窗口里点「下载」`);
    try {
      await Tolk.downloadAppleLanguage(name);
    } catch (e) {
      toast(String(e instanceof Error ? e.message : e));
    }
    downloading.delete(name);
    await checkPacks([name]);
    if (packs[name] === "installed") toast(`「${name}」语言包已就绪`);
  }

  function label(key: number, mods: number) {
    if (win) {
      const parts = [mods & CONTROL ? "Ctrl" : "", mods & OPTION ? "Alt" : "", mods & SHIFT ? "Shift" : "", mods & CMD ? "Win" : ""].filter(Boolean);
      return [...parts, KEY_NAMES[key] ?? `Key ${key}`].join("+");
    }
    return (mods & CONTROL ? "⌃" : "") + (mods & OPTION ? "⌥" : "") + (mods & SHIFT ? "⇧" : "") + (mods & CMD ? "⌘" : "") + (KEY_NAMES[key] ?? `Key ${key}`);
  }

  // Wording that differs by system.
  $("#ia-intro").textContent = win
    ? "在输入框里选中自己写的文字，按快捷键就能看到几种语言的译文，选一个直接替换原文。"
    : "在输入框里选中自己写的文字，按快捷键就能看到几种语言的译文，选一个直接替换原文；替换不了的应用会把译文复制好。需要「辅助功能」权限。";
  $("#ia-auto-text").textContent = win ? "选中文字后自动弹出译文（实验功能，部分应用无效）" : "选中文字后自动弹出译文（实验功能）";

  function drawTargets() {
    const full = targetList.length >= MAX_TARGETS;
    targets.replaceChildren(
      ...langs.map((name) => {
        const on = targetList.includes(name);
        const l = document.createElement("label");
        const box = document.createElement("input");
        box.type = "checkbox";
        box.checked = on;
        box.disabled = on ? targetList.length <= 1 : full;
        l.classList.toggle("off", box.disabled && !on);
        box.addEventListener("change", async () => {
          targetList = box.checked ? [...targetList, name] : targetList.filter((x) => x !== name);
          drawTargets();
          if (box.checked && apple) {
            await checkPacks([name]);
            if (packs[name] === "supported") void download(name);
          }
        });
        l.append(box, document.createTextNode(name));
        if (on && apple && (packs[name] === "supported" || downloading.has(name))) {
          const chip = document.createElement("button");
          chip.type = "button";
          chip.className = "pack-chip";
          chip.textContent = downloading.has(name) ? "下载中…" : "需下载";
          chip.title = "下载 Apple 本地翻译语言包";
          chip.disabled = downloading.has(name);
          chip.addEventListener("click", (e) => {
            e.preventDefault();
            void download(name);
          });
          l.append(chip);
        }
        return l;
      }),
    );
    count.textContent =
      `已选 ${targetList.length} 个（最多 ${MAX_TARGETS} 个），按勾选顺序排列。` +
      (targetList.length > RECOMMENDED_TARGETS ? `建议不超过 ${RECOMMENDED_TARGETS} 个，挑起来更快。` : "") +
      (targetList.some((n) => packs[n] === "supported") ? "标着「需下载」的语言要先下载语言包，点一下即可。" : "");
  }

  function drawScope() {
    const list = scope.value === "allowlist";
    $("#ia-allow-row").hidden = !list;
    const names = win ? "每行写一个应用名称或程序文件名，例如 WeChat.exe。" : "每行写一个应用名称，例如 微信。";
    $("#ia-list-hint").textContent = `${names}密码输入框永远不会触发。`;
  }

  function drawShortcuts(key: number, mods: number) {
    const opts = SHORTCUTS.slice();
    if (!opts.some(([k, m]) => k === key && m === mods)) opts.push([key, mods]); // an imported custom one stays selectable
    shortcut.replaceChildren(...opts.map(([k, m]) => new Option(label(k, m), `${k}-${m}`)));
    shortcut.value = `${key}-${mods}`;
  }

  async function refresh() {
    const st = await Tolk.getInputAssist();
    // On the Mac the helper may not be running at all.
    const running = win || (await Tolk.macStatus()).running;
    status.className = "muted ia-status";
    let text = "";
    if (!running) text = "后台组件没有运行，请重启 Tolk";
    else if (savedEnabled) {
      if (st.active) {
        text = "已就绪";
        status.classList.add("ok");
      } else {
        text = st.error || "没能启动";
        status.classList.add("no");
      }
    }
    status.textContent = text;
    test.toggleAttribute("disabled", !(running && savedEnabled && st.active));
  }

  enabled.addEventListener("change", () => (body.hidden = !enabled.checked));
  scope.addEventListener("change", drawScope);
  // Clicking here makes Tolk the frontmost app, so triggering at once would try
  // to read Tolk's own window. Close the sheet, give the user a moment to switch
  // to the app that holds the selection, then trigger.
  test.addEventListener("click", () => {
    close();
    toast("5 秒后开始测试：请切到其它应用并选中一段文字");
    window.setTimeout(async () => {
      try {
        await Tolk.inputAssistTrigger();
      } catch (e) {
        toast(String(e instanceof Error ? e.message : e));
      }
    }, 5000);
  });

  return {
    async load() {
      try {
        langs = await Tolk.languages();
        const ia = await Tolk.getInputAssist();
        const c = ia.settings;
        loaded = true;
        savedEnabled = c.enabled;
        enabled.checked = c.enabled;
        body.hidden = !c.enabled;
        drawShortcuts(c.shortcutKeyCode, c.shortcutModifiers);
        targetList = c.targets.slice();
        apple = !win && (await Tolk.getSettings()).engine === "apple";
        packs = {};
        drawTargets();
        void checkPacks(targetList);
        scope.value = c.scope;
        block.value = c.blocklist;
        allow.value = c.allowlist;
        auto.checked = c.autoShow;
        badge.checked = c.cacheBadge;
        drawScope();
      } catch (e) {
        loaded = false; // do not save a form that never loaded
        toast(String(e instanceof Error ? e.message : e));
      }
      await refresh().catch(() => {});
      window.clearInterval(timer);
      timer = window.setInterval(() => {
        if (!$("#settings").hasAttribute("open")) return window.clearInterval(timer);
        void refresh().catch(() => {});
      }, 2000); // a grant or a freed shortcut shows up without reopening
    },
    async save() {
      window.clearInterval(timer);
      if (!loaded) return;
      const [key, mods] = shortcut.value.split("-").map(Number) as [number, number];
      const next: IaConfig = {
        enabled: enabled.checked,
        targets: targetList,
        shortcutKeyCode: key,
        shortcutModifiers: mods,
        scope: scope.value,
        blocklist: block.value,
        allowlist: allow.value,
        autoShow: auto.checked,
        cacheBadge: badge.checked,
      };
      const st = await Tolk.setInputAssist(next);
      savedEnabled = st.settings.enabled;
      if (st.settings.enabled && !st.active && st.error) toast(`输入助手：${st.error}`);
    },
  };
}
