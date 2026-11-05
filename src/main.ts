import "./onboarding.css";
import { Tolk, events } from "./mygo";
import { THEMES, applyLook, initLook } from "./look";
import { enhanceSelect } from "./select";
import { engineList } from "./engines";
import { initSettings } from "./settings";
import { macPane } from "./mac";
import { inputAssistPane } from "./inputassist";
import { langName } from "./langname";
import { initOnboarding } from "./onboarding";
import { initUpdate } from "./update";
import { drawKeys, playDouble, pressKeys } from "./keys";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

const source = $<HTMLSelectElement>("#source");
const target = $<HTMLSelectElement>("#target");
const engineSel = $<HTMLSelectElement>("#engine");
const input = $<HTMLTextAreaElement>("#input");
const output = $<HTMLDivElement>("#output");
const empty = $("#empty");
const status = $("#status");
const emptyTitle = $("#empty strong");
const emptyHint = $("#empty > span");
const emptyKeys = $("#empty-keys");
const detected = $("#detected");
const copyBtn = $<HTMLButtonElement>("#copy");
const retryBtn = $<HTMLButtonElement>("#retry");
const pasteBtn = $<HTMLButtonElement>("#paste");
const clearBtn = $<HTMLButtonElement>("#clear");
const swapBtn = $("#swap");
const dot = $("#dot");

/** Restart a one-shot animation class. */
function replay(el: Element, cls: string) {
  el.classList.remove("land", "swap-in", "swap-in-left", "swap-in-right");
  void (el as HTMLElement).offsetWidth;
  el.classList.add(cls);
}

const AUTO = "自动检测";
// The config keeps the Chinese labels; the pickers show the Mac app's wording.
const SHOWN: Record<string, string> = { 简体中文: "中文（简体）", 繁体中文: "中文（繁体）" };
const shown = (name: string) => SHOWN[name] ?? name;

function load(key: string, fallback: string): string {
  try {
    return localStorage.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}
function save(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* storage can be unavailable */
  }
}

let toastTimer: number | undefined;
function toast(message: string) {
  const el = $("#toast");
  el.textContent = message;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => (el.hidden = true), 1800);
}

function fill(sel: HTMLSelectElement, items: [string, string][]) {
  sel.replaceChildren(...items.map(([value, label]) => new Option(label, value)));
}

enhanceSelect(source);
enhanceSelect(target);
enhanceSelect(engineSel);
// One round trip instead of four in a row: on a slow PC each call to Go is
// noticeable, and the toolbar stays empty until all of them are back.
const [langs, about, initialPrefs, initialSettings] = await Promise.all([
  Tolk.languages(),
  Tolk.about(),
  initLook(),
  Tolk.getSettings(),
]);
fill(source, [AUTO, ...langs].map((n) => [n, shown(n)]));
fill(target, langs.map((n) => [n, shown(n)]));
fill(engineSel, engineList(about.os));
let prefs = initialPrefs;
source.value = prefs.sourceLang;
target.value = prefs.targetLang;

// The toolbar button shows the theme in use: monitor, sun, moon or heart.
const THEME_ICONS: Record<string, string> = {
  system: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
  light: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  dark: '<path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/>',
  pink: '<path d="M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10z"/>',
};
const themeBtn = $("#theme");
function showTheme(theme: string) {
  const id = THEME_ICONS[theme] ? theme : "system";
  const name = THEMES.find(([t]) => t === id)![1];
  themeBtn.querySelector("svg")!.innerHTML = THEME_ICONS[id]!;
  themeBtn.title = `主题：${name}（点击切换）`;
}
showTheme(prefs.theme);
// The tray / menu bar can switch Mini mode too.
events.prefsChanged.on((p) => {
  prefs = p;
  showTheme(p.theme);
  showMini();
  drawHint();
});
themeBtn.addEventListener("click", async () => {
  const order = THEMES.map(([id]) => id);
  const next = order[(order.indexOf(prefs.theme) + 1) % order.length]!;
  try {
    await Tolk.setAppearance(next, prefs.sourceFont, prefs.targetFont);
    prefs = await Tolk.getPrefs();
    applyLook(prefs);
    showTheme(prefs.theme);
    toast(`主题：${THEMES.find(([id]) => id === next)![1]}`);
  } catch (e) {
    toast(String(e instanceof Error ? e.message : e));
  }
});
const miniBtn = $("#mini");
function showMini() {
  miniBtn.classList.toggle("active", prefs.miniMode);
  miniBtn.title = prefs.miniMode ? "关闭 Mini 模式" : "开启 Mini 模式";
}
showMini();
miniBtn.addEventListener("click", async () => {
  try {
    await Tolk.setMini(!prefs.miniMode);
    prefs = await Tolk.getPrefs();
    showMini();
    toast(prefs.miniMode ? "Mini 模式已开启：双击复制会在鼠标旁显示译文" : "Mini 模式已关闭");
  } catch (e) {
    toast(String(e instanceof Error ? e.message : e));
  }
});

async function saveLanguages() {
  try {
    await Tolk.setLanguages(source.value, target.value);
  } catch (e) {
    toast(`语言设置没保存上：${e instanceof Error ? e.message : e}`);
  }
}

async function refreshSettings() {
  const s = await Tolk.getSettings();
  engineSel.value = s.engine;
  return s;
}
engineSel.value = initialSettings.engine;

let seq = 0;
let timer: number | undefined;
let detectedName = "";

// The empty stage teaches the gesture that fills it, on this system's keys and
// at the speed the settings ask for.
const hint = { title: "", text: "" };
function drawHint() {
  const mac = about.os === "darwin";
  const mod = mac ? "⌘" : "Ctrl";
  if (prefs.doubleCopy) {
    drawKeys(emptyKeys, [mod, "C"], !mac);
    hint.title = "选中文字，连按两次复制";
    hint.text = "在任何应用里都行，译文会落在这里。";
  } else {
    drawKeys(emptyKeys, [mod, mac ? "⇧" : "Shift", "C"], !mac);
    hint.title = "按这组键，翻译剪贴板里的文字";
    hint.text = "也可以直接在左边输入。";
  }
  if (!empty.hidden && !empty.classList.contains("busy")) showEmpty(true);
}
function playHint(wait = 0) {
  if (prefs.doubleCopy) playDouble(emptyKeys, prefs.doubleCopyMs, wait);
  else pressKeys(emptyKeys);
}

function showEmpty(on: boolean, busy = false) {
  empty.hidden = !on;
  empty.classList.toggle("busy", busy);
  emptyTitle.textContent = busy ? "翻译中…" : hint.title;
  emptyHint.textContent = busy ? "" : hint.text;
}

/** What the result can do right now: copy it, or try again. */
function drawResultActions(state: "none" | "done" | "error") {
  copyBtn.hidden = state !== "done";
  retryBtn.hidden = state !== "error";
}

function showDetected(name: string) {
  detected.textContent = name;
  detected.hidden = !name;
}

async function run() {
  const text = input.value;
  const mine = ++seq;
  const hadError = output.classList.contains("error");
  output.classList.remove("error");
  if (!text.trim()) {
    output.textContent = "";
    output.classList.remove("busy");
    status.textContent = "";
    showEmpty(true);
    drawResultActions("none");
    return;
  }
  // First translation: the placeholder says so. Later ones: the old text fades.
  const first = !output.textContent || hadError;
  if (first) output.textContent = "";
  showEmpty(first, first);
  output.classList.add("busy");
  status.textContent = "";
  drawResultActions("none");
  try {
    const res = await Tolk.translate(text, source.value, target.value);
    if (mine !== seq) return; // a newer request replaced this one
    showEmpty(false);
    output.classList.remove("busy");
    output.textContent = res.text;
    if (first) replay(output, "land");
    dot.classList.remove("err");
    detectedName = langName(res.detected);
    showDetected(source.value === AUTO ? detectedName : "");
    status.textContent = `${(res.durationMs / 1000).toFixed(1)} 秒`;
    drawResultActions("done");
  } catch (e) {
    if (mine !== seq) return;
    showEmpty(false);
    output.classList.remove("busy");
    output.classList.add("error");
    output.textContent = String(e instanceof Error ? e.message : e);
    dot.classList.add("err");
    status.textContent = "";
    drawResultActions("error");
  }
}

function schedule(delay = 380) {
  clearTimeout(timer);
  timer = window.setTimeout(run, delay);
}

function clearTranslationState() {
  // Invalidate in-flight responses before changing the selected service.
  ++seq;
  clearTimeout(timer);
  output.textContent = "";
  output.classList.remove("error", "busy");
  status.textContent = "";
  dot.classList.remove("err");
  detectedName = "";
  showDetected("");
  showEmpty(true);
  drawResultActions("none");
}

function drawInputState() {
  $("#count").textContent = input.value ? `${input.value.length} 字` : "";
  clearBtn.hidden = !input.value;
  pasteBtn.hidden = !!input.value;
}

input.addEventListener("input", () => {
  drawInputState();
  if (!input.value.trim()) {
    clearTranslationState();
    return;
  }
  schedule();
});
source.addEventListener("change", () => {
  if (source.value !== AUTO) showDetected("");
  void saveLanguages();
  schedule(0);
});
target.addEventListener("change", () => { void saveLanguages(); schedule(0); });
engineSel.addEventListener("change", async () => {
  clearTranslationState();
  try {
    await Tolk.setEngine(engineSel.value);
    dot.classList.remove("err");
    if (engineSel.value === "achord_builtin") {
      const localPlugin = await Tolk.pluginStatus();
      if (!localPlugin.installed) { await settings.open("service"); return; }
      // Installed engines remain usable while release discovery is offline.
      void Tolk.checkPlugin().then(async (plugin) => {
        if (engineSel.value !== "achord_builtin") return;
        if (plugin.latest && plugin.latest !== plugin.installed && !document.querySelector("dialog[open]")) {
          await settings.open("service");
        }
      }).catch(e => toast(`插件更新检查失败：${String(e)}`));
    }
    schedule(0);
  } catch (e) {
    toast(String(e instanceof Error ? e.message : e));
    await refreshSettings();
  }
});

// The pair trades places and so does the text: what was the translation becomes
// the thing to translate, and it crosses the seam the button sits on.
let swapTurn = 0;
const engineLabel = (name: string) => (name === "中文" ? "简体中文" : langs.find((n) => shown(n) === name || n === name));
swapBtn.addEventListener("click", () => {
  const from = source.value === AUTO ? engineLabel(detectedName) : source.value;
  if (!from) return toast("还不知道原文是什么语言，先翻译一句再交换");
  const to = target.value;
  const translated = !output.classList.contains("error") && !output.classList.contains("busy") ? output.textContent ?? "" : "";
  source.value = to;
  target.value = from;
  swapTurn += 180;
  swapBtn.querySelector("svg")!.style.transform = `rotate(${swapTurn}deg)`;
  replay($("#source-pill"), "swap-in-right");
  replay($("#target-pill"), "swap-in-left");
  showDetected("");
  void saveLanguages();
  if (translated) {
    ++seq; // whatever is still on its way answers the old direction
    output.textContent = input.value;
    input.value = translated;
    drawInputState();
    replay(input, "swap-in");
    replay(output, "swap-in");
  }
  schedule(0);
});

clearBtn.addEventListener("click", () => {
  input.value = "";
  input.dispatchEvent(new Event("input"));
  input.focus();
  playHint();
});

pasteBtn.addEventListener("click", async () => {
  try {
    const text = await Tolk.clipboardText();
    if (!text.trim()) return toast("剪贴板里没有文字");
    input.value = text;
    drawInputState();
    replay(input, "land");
    schedule(0);
    input.focus();
  } catch (e) {
    toast(String(e instanceof Error ? e.message : e));
  }
});

retryBtn.addEventListener("click", () => schedule(0));

// Copying looks like what it is: the whole translation is selected for a moment.
let copiedTimer: number | undefined;
copyBtn.addEventListener("click", async () => {
  const text = output.textContent;
  if (!text || output.classList.contains("error")) return;
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    return toast("复制失败");
  }
  const mark = document.createElement("mark");
  mark.textContent = text;
  output.replaceChildren(mark);
  mark.addEventListener("animationend", () => {
    if (mark.isConnected) output.textContent = text;
  });
  copyBtn.classList.add("copied");
  $("#copy-text").textContent = "已复制";
  clearTimeout(copiedTimer);
  copiedTimer = window.setTimeout(() => {
    copyBtn.classList.remove("copied");
    $("#copy-text").textContent = "复制";
  }, 1400);
});

events.copied.on((text) => {
  if (!text) return;
  input.value = text;
  drawInputState();
  replay(input, "land"); // it just came in from another app
  schedule(0);
});

events.macNotice.on((m) => toast(m));

for (const id of ["#s-vendor", "#s-dc-ms", "#ia-shortcut", "#ia-scope", "#c-level"]) enhanceSelect($<HTMLSelectElement>(id));
const settings = initSettings({
  os: about.os,
  toast,
  mac: about.os === "darwin" ? macPane(toast, () => $("#s-cancel").click()) : undefined,
  ia: about.os === "darwin" || about.os === "windows" ? inputAssistPane(about.os, toast, () => $("#s-cancel").click()) : undefined,
  saved: async (next) => {
    clearTranslationState();
    prefs = next;
    showMini();
    showTheme(next.theme);
    drawHint();
    await refreshSettings();
    schedule(0);
  },
});

// From the tray / menu bar / Dock menu.
events.menuCommand.on(async (c) => {
  if (c === "settings") await settings.open();
  else if (c === "update") {
    await settings.open("about");
    $<HTMLButtonElement>("#a-check").click();
  }
});

drawHint();
showEmpty(true);
drawInputState();
document.querySelector(".app")!.removeAttribute("inert");
document.documentElement.classList.remove("booting", "startup-failed");
window.dispatchEvent(new Event("tolk-ready"));
input.focus();
playHint(520);
initOnboarding(about.os, () => settings.open("ia"), toast);
await initUpdate(toast);
