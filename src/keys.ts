/** Keycaps, the ones the trial teaches with, and their double press. */

const calm = () => matchMedia("(prefers-reduced-motion: reduce)").matches;

/** Draw one cap per key. Windows writes its chords with a plus between the keys. */
export function drawKeys(el: HTMLElement, parts: string[], win: boolean) {
  el.setAttribute("aria-label", `快捷键 ${parts.join(" ")}`);
  el.replaceChildren(
    ...parts.flatMap((part, i) => {
      const cap = document.createElement("kbd");
      cap.textContent = part;
      cap.style.setProperty("--i", String(i));
      if (part === "Space") cap.className = "wide";
      if (i === 0 || !win) return [cap];
      const plus = document.createElement("span");
      plus.textContent = "+";
      return [plus, cap];
    }),
  );
}

const DOWN = { transform: "translateY(3px)", borderBottomWidth: "1px" };
const UP = { transform: "translateY(0)", borderBottomWidth: "4px" };

function tap(cap: Element, at: number, hold: number) {
  cap.animate([UP, DOWN, DOWN, UP], {
    duration: hold,
    delay: at,
    easing: "ease-out",
    fill: "backwards",
  });
}

/** One press of the chord: every cap goes down together. */
export function pressKeys(el: HTMLElement) {
  if (calm()) return;
  el.querySelectorAll("kbd").forEach((cap) => tap(cap, 0, 140));
}

/**
 * The gesture at the speed it really has to be done: the modifiers stay down
 * while the last key is tapped twice, `gap` milliseconds apart.
 */
export function playDouble(el: HTMLElement, gap: number, wait = 0) {
  if (calm()) return;
  const caps = [...el.querySelectorAll("kbd")];
  const key = caps[caps.length - 1];
  if (!key) return;
  const tapMs = 150;
  const total = gap + tapMs;
  caps.slice(0, -1).forEach((cap) =>
    cap.animate(
      [
        { ...UP, offset: 0 },
        { ...DOWN, offset: 60 / (total + 120) },
        { ...DOWN, offset: (total + 60) / (total + 120) },
        { ...UP, offset: 1 },
      ],
      { duration: total + 120, delay: wait, fill: "backwards" },
    ),
  );
  tap(key, wait + 60, tapMs);
  tap(key, wait + 60 + gap, tapMs);
}
