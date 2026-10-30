import { Tolk, events } from "./mygo";
import { initLook } from "./look";
import { langName } from "./langname";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;
const body = $("#body");
const meta = $("#meta");
const foot = $("#foot");

const SHOWN: Record<string, string> = { 简体中文: "中文（简体）", 繁体中文: "中文（繁体）" };
const shown = (name: string) => SHOWN[name] ?? name;

void initLook();

let dismissTimer: number | undefined;
let hovering = false;
let remaining = 0;
let deadline = 0;

function stopDismiss() {
  clearTimeout(dismissTimer);
  dismissTimer = undefined;
}

function scheduleDismiss(ms: number) {
  stopDismiss();
  remaining = ms;
  if (hovering) return;
  deadline = Date.now() + ms;
  dismissTimer = window.setTimeout(() => void Tolk.hideMini(), ms);
}

document.body.addEventListener("mouseenter", () => {
  hovering = true;
  if (dismissTimer !== undefined) remaining = Math.max(300, deadline - Date.now());
  stopDismiss();
});
document.body.addEventListener("mouseleave", () => {
  hovering = false;
  if (remaining > 0) scheduleDismiss(remaining);
});

addEventListener("keydown", (e) => {
  if (e.key === "Escape") void Tolk.hideMini();
});

$("#copy").addEventListener("click", async () => {
  if (body.classList.contains("error") || body.classList.contains("wait")) return;
  try {
    await navigator.clipboard.writeText(body.textContent ?? "");
    meta.textContent = "已复制";
  } catch {
    meta.textContent = "复制失败";
  }
});

function fit() {
  // The window is a bit taller than the bubble: title bar chrome, borders.
  requestAnimationFrame(() => void Tolk.resizeMini(Math.ceil($("#bubble").getBoundingClientRect().height) + 2));
}

events.mini.on((s) => {
  body.className = "mini-body";
  foot.textContent = "";
  if (s.state === "translating") {
    body.classList.add("wait");
    body.textContent = "翻译中…";
    meta.textContent = "";
    stopDismiss();
  } else if (s.state === "error") {
    body.classList.add("error");
    body.textContent = s.text;
    meta.textContent = "";
    scheduleDismiss(8000);
  } else {
    body.textContent = s.text;
    const from = langName(s.detected) || (s.source === "自动检测" ? "" : shown(s.source));
    meta.textContent = [from, `→ ${shown(s.target)}`].filter(Boolean).join(" ");
    scheduleDismiss(8000);
  }
  fit();
});
