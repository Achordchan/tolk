import { Tolk, events } from "./mygo";
import { THEMES, applyLook, initLook } from "./look";
import { enhanceSelect } from "./select";
import { engineList } from "./engines";
import { initSettings } from "./settings";
import { macPane } from "./mac";
import { inputAssistPane } from "./inputassist";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

const source = $<HTMLSelectElement>("#source");
const target = $<HTMLSelectElement>("#target");
const engineSel = $<HTMLSelectElement>("#engine");
const input = $<HTMLTextAreaElement>("#input");
const output = $<HTMLDivElement>("#output");
const empty = $("#empty");
const status = $("#status");
const dot = $("#dot");

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
const langs = await Tolk.languages();
fill(source, [AUTO, ...langs].map((n) => [n, shown(n)]));
fill(target, langs.map((n) => [n, shown(n)]));
const about = await Tolk.about();
fill(engineSel, engineList(about.os));
let prefs = await initLook();
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
events.prefsChanged.on((p) => showTheme(p.theme));
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
    toast(String(e instanceof Error ? e.message : e));
  }
}

async function refreshSettings() {
  const s = await Tolk.getSettings();
  engineSel.value = s.engine;
  return s;
}
await refreshSettings();

let seq = 0;
let timer: number | undefined;

function showEmpty(on: boolean) {
  empty.hidden = !on;
}

async function run() {
  const text = input.value;
  const mine = ++seq;
  output.classList.remove("error");
  if (!text.trim()) {
    output.textContent = "";
    status.textContent = "";
    showEmpty(true);
    return;
  }
  showEmpty(false);
  status.textContent = "翻译中…";
  try {
    const res = await Tolk.translate(text, source.value, target.value);
    if (mine !== seq) return; // a newer request replaced this one
    output.textContent = res.text;
    dot.classList.remove("err");
    status.textContent = `${(res.durationMs / 1000).toFixed(1)} 秒` + (res.detected ? ` · 检测到 ${res.detected}` : "");
  } catch (e) {
    if (mine !== seq) return;
    output.classList.add("error");
    output.textContent = String(e instanceof Error ? e.message : e);
    dot.classList.add("err");
    status.textContent = "";
  }
}

function schedule(delay = 380) {
  clearTimeout(timer);
  timer = window.setTimeout(run, delay);
}

input.addEventListener("input", () => {
  $("#count").textContent = input.value ? `${input.value.length} 字` : "";
  schedule();
});
source.addEventListener("change", () => { void saveLanguages(); schedule(0); });
target.addEventListener("change", () => { void saveLanguages(); schedule(0); });
engineSel.addEventListener("change", async () => {
  try {
    await Tolk.setEngine(engineSel.value);
    dot.classList.remove("err");
    schedule(0);
  } catch (e) {
    toast(String(e instanceof Error ? e.message : e));
    await refreshSettings();
  }
});

$("#swap").addEventListener("click", () => {
  if (source.value === AUTO) return toast("源语言是自动检测，无法交换");
  [source.value, target.value] = [target.value, source.value];
  void saveLanguages();
  schedule(0);
});

$("#clear").addEventListener("click", () => {
  input.value = "";
  input.dispatchEvent(new Event("input"));
  input.focus();
});

$("#copy").addEventListener("click", async () => {
  if (!output.textContent || output.classList.contains("error")) return;
  try {
    await navigator.clipboard.writeText(output.textContent);
    toast("已复制译文");
  } catch {
    toast("复制失败");
  }
});

events.copied.on((text) => {
  if (!text) return;
  input.value = text;
  input.dispatchEvent(new Event("input"));
  schedule(0);
});

events.macNotice.on((m) => toast(m));

for (const id of ["#s-engine", "#s-vendor", "#s-dc-ms", "#ia-shortcut", "#ia-scope", "#c-level"]) enhanceSelect($<HTMLSelectElement>(id));
initSettings({
  os: about.os,
  toast,
  mac: about.os === "darwin" ? macPane(toast, () => $("#s-cancel").click()) : undefined,
  ia: about.os === "darwin" || about.os === "windows" ? inputAssistPane(about.os, toast, () => $("#s-cancel").click()) : undefined,
  saved: async (next) => {
    prefs = next;
    showMini();
    showTheme(next.theme);
    await refreshSettings();
    schedule(0);
  },
});

showEmpty(true);
input.focus();
