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
  const p = await Tolk.getPrefs();
  applyLook(p);
  events.prefsChanged.on((next) => applyLook(next));
  return p;
}
