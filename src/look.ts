import { Tolk, events } from "./mygo";

type Prefs = Awaited<ReturnType<typeof Tolk.getPrefs>>;

export const THEMES: [string, string][] = [
  ["system", "跟随系统"],
  ["light", "浅色"],
  ["dark", "深色"],
  ["pink", "粉色"],
];

/** Theme and text sizes live in the shared config; every window applies them. */
export function applyLook(p: Pick<Prefs, "theme" | "sourceFont" | "targetFont">) {
  const root = document.documentElement;
  if (p.theme && p.theme !== "system") root.dataset.theme = p.theme;
  else delete root.dataset.theme;
  root.style.setProperty("--src-size", `${p.sourceFont}px`);
  root.style.setProperty("--dst-size", `${p.targetFont}px`);
}

/** Apply the saved look now and whenever another window changes it. */
export async function initLook(): Promise<Prefs> {
  // The webview's own menu only offers Reload, which empties the page; text
  // fields keep theirs for paste.
  document.addEventListener("contextmenu", (e) => {
    const t = e.target as HTMLElement;
    if (!t.closest("textarea, input")) e.preventDefault();
  });
  document.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "r") e.preventDefault(); // no reload either
  });
  const p = await Tolk.getPrefs();
  applyLook(p);
  events.prefsChanged.on((next) => applyLook(next));
  return p;
}
