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

  function label(key: number, mods: number) {
    if (win) {
      const parts = [mods & CONTROL ? "Ctrl" : "", mods & OPTION ? "Alt" : "", mods & SHIFT ? "Shift" : "", mods & CMD ? "Win" : ""].filter(Boolean);
      return [...parts, KEY_NAMES[key] ?? `Key ${key}`].join("+");
    }
    return (mods & CONTROL ? "⌃" : "") + (mods & OPTION ? "⌥" : "") + (mods & SHIFT ? "⇧" : "") + (mods & CMD ? "⌘" : "") + (KEY_NAMES[key] ?? `Key ${key}`);
  }

  // Wording that differs by system.
  $("#ia-intro").textContent = win
    ? "选中文字后按快捷键查看翻译候选，选一种就替换回输入框（用粘贴完成，用完会还原你的剪贴板）。微信这类不提供选区接口的应用，Tolk 会临时按一次 Ctrl+C 来读取选中的文字。"
    : "选中文字后按快捷键查看翻译候选；支持精确写入的编辑器可原位替换，其它应用会复制译文。需要辅助功能权限。";
  $("#ia-auto-text").textContent = win ? " 选区完成后自动显示候选（实验，只对提供选区接口的应用有效）" : " 选区完成后自动显示候选（实验）";

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
        box.addEventListener("change", () => {
          targetList = box.checked ? [...targetList, name] : targetList.filter((x) => x !== name);
          drawTargets();
        });
        l.append(box, document.createTextNode(name));
        return l;
      }),
    );
    count.textContent =
      `已选 ${targetList.length} 个，最多 ${MAX_TARGETS} 个，按勾选的先后顺序展示，默认高亮第一项。` +
      (targetList.length > RECOMMENDED_TARGETS ? `候选语言较多可能降低选择效率，建议不超过 ${RECOMMENDED_TARGETS} 个。` : "");
  }

  function drawScope() {
    const list = scope.value === "allowlist";
    $("#ia-list-title").textContent = "黑名单";
    $("#ia-allow-row").hidden = !list;
    const names = win ? "可填应用名或程序文件名（如 WeChat.exe）" : "可填应用名或 Bundle ID";
    $("#ia-list-hint").textContent = list
      ? `每行一个，${names}。黑名单优先于「仅在这些应用启用」；密码框和安全输入状态永远跳过。`
      : `每行一个，${names}。密码框和安全输入状态永远跳过。`;
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
    if (!running) text = "原生助手未运行";
    else if (savedEnabled) {
      if (st.active) {
        text = "输入助手已启动";
        status.classList.add("ok");
      } else {
        text = st.error || "输入助手未启动";
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
        drawTargets();
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
