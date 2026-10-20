import { Tolk, events } from "./mygo";

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
const ENGINES: [string, string][] = [
  ["google", "Google 翻译"],
  ["microsoft", "微软翻译"],
  ["deepl", "DeepL"],
  ["openai_compat", "AI 翻译"],
  ["achord_builtin", "Achord 内置引擎"],
];
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

// Theme: follow the system until the user picks one.
const root = document.documentElement;
const savedTheme = load("theme", "");
if (savedTheme) root.dataset.theme = savedTheme;
$("#theme").addEventListener("click", () => {
  const dark = root.dataset.theme
    ? root.dataset.theme === "dark"
    : matchMedia("(prefers-color-scheme: dark)").matches;
  root.dataset.theme = dark ? "light" : "dark";
  save("theme", root.dataset.theme);
});

function fill(sel: HTMLSelectElement, items: [string, string][]) {
  sel.replaceChildren(...items.map(([value, label]) => new Option(label, value)));
}

const langs = await Tolk.languages();
fill(source, [AUTO, ...langs].map((n) => [n, shown(n)]));
fill(target, langs.map((n) => [n, shown(n)]));
fill(engineSel, ENGINES);
source.value = load("source", AUTO);
target.value = load("target", "简体中文");

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
source.addEventListener("change", () => { save("source", source.value); schedule(0); });
target.addEventListener("change", () => { save("target", target.value); schedule(0); });
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
  save("source", source.value);
  save("target", target.value);
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

// ---- settings sheet ----
const dlg = $<HTMLDialogElement>("#settings");
const sEngine = $<HTMLSelectElement>("#s-engine");
const sVendor = $<HTMLSelectElement>("#s-vendor");
const sBase = $<HTMLInputElement>("#s-base");
const sModel = $<HTMLInputElement>("#s-model");
const sAiKey = $<HTMLInputElement>("#s-ai-key");
const sDeeplKey = $<HTMLInputElement>("#s-deepl-key");
const sMsKey = $<HTMLInputElement>("#s-ms-key");
const sMsRegion = $<HTMLInputElement>("#s-ms-region");
const sError = $("#settings-error");

type Cfg = Awaited<ReturnType<typeof Tolk.getSettings>>;
let cfg: Cfg;

function syncSections() {
  document.querySelectorAll<HTMLElement>("fieldset[data-for]").forEach((f) => {
    f.hidden = f.dataset.for !== sEngine.value;
  });
}

function fillVendor() {
  const p = cfg.profiles?.[sVendor.value];
  const preset = cfg.vendors.find((v) => v.name === sVendor.value);
  sBase.value = p?.baseUrl || preset?.baseUrl || "";
  sModel.value = p?.model || preset?.model || "";
  sAiKey.value = "";
  sAiKey.placeholder = p?.hasKey ? "已保存，留空则不修改" : "";
}

sEngine.addEventListener("change", syncSections);
sVendor.addEventListener("change", fillVendor);

$("#open-settings").addEventListener("click", async () => {
  cfg = await Tolk.getSettings();
  fill(sEngine, ENGINES);
  sEngine.value = cfg.engine;
  fill(sVendor, cfg.vendors.map((v) => [v.name, v.name]));
  sVendor.value = cfg.vendor;
  fillVendor();
  sDeeplKey.value = "";
  sDeeplKey.placeholder = cfg.deeplHasKey ? "已保存，留空则不修改" : "";
  sMsKey.value = "";
  sMsKey.placeholder = cfg.microsoftHasKey ? "已保存，留空则不修改" : "";
  sMsRegion.value = cfg.microsoftRegion;
  $("#settings-note").textContent = "与旧版大佐翻译官共用同一份设置，密钥只保存在本机。";
  sError.hidden = true;
  syncSections();
  dlg.showModal();
});

$("#s-cancel").addEventListener("click", () => dlg.close());

$("#settings-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  try {
    await Tolk.saveSettings({
      engine: sEngine.value,
      vendor: sVendor.value,
      baseUrl: sBase.value,
      model: sModel.value,
      aiKey: sAiKey.value,
      deeplKey: sDeeplKey.value,
      microsoftKey: sMsKey.value,
      microsoftRegion: sMsRegion.value,
    });
    dlg.close();
    await refreshSettings();
    toast("设置已保存");
    schedule(0);
  } catch (err) {
    sError.textContent = String(err instanceof Error ? err.message : err);
    sError.hidden = false;
  }
});

showEmpty(true);
input.focus();
