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

$("#to-main").addEventListener("click", () => void Tolk.miniToMain());

$("#copy").addEventListener("click", async () => {
  if (body.classList.contains("error") || body.classList.contains("wait")) return;
  try {
    await navigator.clipboard.writeText(body.textContent ?? "");
    meta.textContent = "已复制";
  } catch {
    meta.textContent = "复制失败";
  }
});

// The bubble's width limits; mini.go clamps to the same ones.
const MIN_W = 300;
const MAX_W = 560;
const bubble = $("#bubble");
let width = MIN_W;

// The window is a bit taller than the bubble: title bar chrome, borders.
const height = () => Math.ceil(bubble.getBoundingClientRect().height) + 2;

function fit() {
  requestAnimationFrame(() => {
    // Unwrapped, the text says how wide it wants to be; a long one stops at
    // MAX_W and wraps. The height is then measured at the width it will get.
    bubble.style.width = "max-content";
    const natural = Math.ceil(bubble.getBoundingClientRect().width) + 4;
    width = Math.max(MIN_W, Math.min(natural, MAX_W, screen.availWidth - 16));
    bubble.style.width = `${width}px`;
    const h = height();
    bubble.style.width = "";
    void Tolk.resizeMini(width, h);
  });
}

// The page can end up a little narrower than asked (window borders, a small
// screen): measure again at the real width so the last line is not cut off.
// A width far from the one asked for means mini.go reset the bubble for a
// new copy (still holding the old text); the fit() that follows sizes it.
addEventListener("resize", () => {
  if (Math.abs(innerWidth - width) > 4) return;
  void Tolk.resizeMini(width, height());
});

events.mini.on((s) => {
  body.className = "mini-body";
  foot.textContent = "";
  if (s.state === "translating") {
    body.classList.add("wait");
    body.textContent = "翻译中…";
    meta.textContent = "";
    stopDismiss();
    remaining = 0;
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

void Tolk.miniReady();
