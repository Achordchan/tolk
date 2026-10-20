import { Tolk, events } from "./mygo";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

const source = $<HTMLSelectElement>("#source");
const target = $<HTMLSelectElement>("#target");
const input = $<HTMLTextAreaElement>("#input");
const output = $<HTMLDivElement>("#output");
const status = $("#status");

const AUTO = "自动检测";
const ENGINE_NAMES: Record<string, string> = {
  google: "Google 翻译",
  microsoft: "微软翻译",
  deepl: "DeepL",
  openai_compat: "AI 翻译",
  achord_builtin: "内置引擎",
};

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

function toast(message: string) {
  const el = $("#toast");
  el.textContent = message;
  el.hidden = false;
  setTimeout(() => (el.hidden = true), 1800);
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

const langs = await Tolk.languages();
for (const name of [AUTO, ...langs]) source.add(new Option(name, name));
for (const name of langs) target.add(new Option(name, name));
source.value = load("source", AUTO);
target.value = load("target", "简体中文");

let seq = 0;
let timer: number | undefined;

async function run() {
  const text = input.value;
  const mine = ++seq;
  output.classList.remove("error");
  if (!text.trim()) {
    output.textContent = "";
    status.textContent = "";
    return;
  }
  status.textContent = "翻译中…";
  try {
    const res = await Tolk.translate(text, source.value, target.value);
    if (mine !== seq) return; // a newer request replaced this one
    output.textContent = res.text;
    $("#engine").textContent = ENGINE_NAMES[res.engine] ?? res.engine;
    status.textContent = `${(res.durationMs / 1000).toFixed(1)} 秒` + (res.detected ? ` · 检测到 ${res.detected}` : "");
  } catch (e) {
    if (mine !== seq) return;
    output.classList.add("error");
    output.textContent = String(e instanceof Error ? e.message : e);
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

const cfg = await Tolk.legacyConfig();
$("#legacy").textContent = cfg.found
  ? `已沿用 dazuo-win 的设置（${cfg.engine || "google"}）`
  : "未找到旧版设置，使用默认的 Google 翻译";
const first = ENGINE_NAMES[cfg.engine || "google"];
$("#engine").textContent = first ?? (cfg.engine || "Google 翻译");
input.focus();
