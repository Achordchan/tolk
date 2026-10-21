import { Tolk, events } from "./mygo";
import { initLook } from "./look";

type View = Awaited<ReturnType<typeof Tolk.panelSnapshot>>;

const root = document.getElementById("panel")!;

void initLook();

const ICONS: Record<string, string> = {
  success: '<path d="M5 12.5l4.5 4.5L19 7"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
  warning: '<path d="M12 3l10 18H2z"/><path d="M12 10v5M12 18h.01"/>',
  copied: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>',
};

function el(tag: string, cls: string, text?: string) {
  const e = document.createElement(tag);
  e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function lineCount(reserved: number, selected: boolean) {
  return Math.max(1, Math.min(selected ? 4 : 2, reserved));
}

function drawRow(v: View, i: number) {
  const r = v.rows[i]!;
  const selected = i === v.selected;
  const row = el("div", selected ? "row sel" : "row");
  row.style.top = `${v.tops[i]}px`;
  row.style.height = `${v.heights[i]}px`;
  row.append(el("div", "code", r.label));
  const body = el("div", "text");
  const clamp = lineCount(v.lines, selected);
  switch (r.state.kind) {
    case "loading":
      body.className = "text";
      body.append(el("div", "skeleton"));
      break;
    case "failed": {
      body.append(document.createTextNode("翻译失败"), el("span", "hint", "点击重试"));
      body.title = r.state.message ?? "";
      break;
    }
    default:
      body.textContent = r.state.text ?? "";
      body.style.setProperty("-webkit-line-clamp", String(clamp));
  }
  row.append(body);
  if (v.badge && r.state.cached) {
    const b = el("div", "bolt", "⚡");
    b.title = `本地缓存命中\n引擎：${r.state.engine ?? ""}\n响应：${r.state.latencyMs ?? 0}ms`;
    row.append(b);
  }
  return row;
}

function draw(v: View) {
  if (v.mode === "list") {
    root.replaceChildren(...v.rows.map((_, i) => drawRow(v, i)));
  } else if (v.mode === "message") {
    const n = el("div", `notice ${v.style}`);
    n.innerHTML = `<svg viewBox="0 0 24 24">${ICONS[v.style] ?? ICONS.info}</svg>`;
    n.append(el("span", "", v.message));
    root.replaceChildren(n);
  } else {
    root.replaceChildren();
  }
}

events.panel.on(draw);
// The window can load after the first event was sent.
void Tolk.panelSnapshot().then(draw);
