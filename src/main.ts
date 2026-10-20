import { Tolk, events } from "./mygo";
import { THEMES, initLook } from "./look";
import { engineList } from "./engines";
import { initSettings } from "./settings";
import { macPane } from "./mac";

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

const langs = await Tolk.languages();
fill(source, [AUTO, ...langs].map((n) => [n, shown(n)]));
fill(target, langs.map((n) => [n, shown(n)]));
const about = await Tolk.about();
fill(engineSel, engineList(about.os));
let prefs = await initLook();
source.value = prefs.sourceLang;
target.value = prefs.targetLang;
$("#theme").addEventListener("click", async () => {
  const order = THEMES.map(([id]) => id);
  const next = order[(order.indexOf(prefs.theme) + 1) % order.length]!;
  try {
    await Tolk.setAppearance(next, prefs.sourceFont, prefs.targetFont);
    prefs = await Tolk.getPrefs();
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

initSettings({
  os: about.os,
  toast,
  mac: about.os === "darwin" ? macPane(toast, () => $("#s-cancel").click()) : undefined,
  saved: async (next) => {
    prefs = next;
    showMini();
    await refreshSettings();
    schedule(0);
  },
});

showEmpty(true);
input.focus();
