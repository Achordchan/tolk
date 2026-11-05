import { Tolk } from "./mygo";
import { shortcutKeys } from "./inputassist";
import { drawKeys } from "./keys";

type Page = 1 | 2 | 3;
type Range = { start: number; end: number };

const CODES: Record<string, string> = { 英语: "EN", 日语: "JA", 法语: "FR" };
const OPTION = 2048;
const SHIFT = 512;
const SPACE = 49;

/** Uses the production translator; only the editable starting sentence is illustrative. */
export function initOnboarding(os: string, configure: () => Promise<void>, toast: (message: string) => void) {
  const win = os === "windows";
  const supported = win || os === "darwin";
  const dialog = document.querySelector<HTMLDialogElement>("#onboarding")!;
  const $ = <T extends HTMLElement>(id: string) => dialog.querySelector<T>(id)!;
  const input = $<HTMLTextAreaElement>("#onboarding-input");
  const languages = [...dialog.querySelectorAll<HTMLInputElement>('input[name="onboarding-language"]')];
  const pages = [...dialog.querySelectorAll<HTMLElement>(".onboarding-page")];
  const dots = [...dialog.querySelectorAll<HTMLElement>(".onboarding-steps li")];
  const title = $("#onboarding-title");
  const description = $("#onboarding-description");
  const note = $("#onboarding-note");
  const keys = $("#onboarding-keys");
  const back = $<HTMLButtonElement>("#onboarding-back");
  const primary = $<HTMLButtonElement>("#onboarding-primary");
  const direct = $<HTMLButtonElement>("#onboarding-settings");
  const reopen = document.querySelector<HTMLButtonElement>("#open-onboarding")!;
  const sample = input.value;
  let page: Page = 1;
  let state: "idle" | "loading" | "error" | "done" = "idle";
  let generation = 0;
  let range: Range = { start: 0, end: 0 };
  let result: { text: string; original: string; seconds: string } | undefined;

  const language = () => languages.find((l) => l.checked)!.value;

  function drawLines(text: string, mark: Range) {
    dialog.querySelectorAll<HTMLElement>("[data-line]").forEach((line) => {
      const selected = document.createElement("mark");
      selected.textContent = text.slice(mark.start, mark.end);
      line.replaceChildren(text.slice(0, mark.start), selected, text.slice(mark.end));
    });
  }

  function draw(error?: string) {
    dialog.dataset.page = String(page);
    dialog.dataset.state = state;
    pages.forEach((el, i) => (el.hidden = i !== page - 1));
    dots.forEach((el, i) => (i === page - 1 ? el.setAttribute("aria-current", "step") : el.removeAttribute("aria-current")));
    const busy = state === "loading";
    const done = state === "done";
    note.hidden = page !== 2;
    back.hidden = page === 1 || done;
    back.disabled = busy;
    direct.textContent = done ? "重新试用" : "直接去设置";
    languages.forEach((l) => (l.disabled = busy));
    primary.disabled = busy || (page === 1 && !input.value.trim());
    description.classList.toggle("onboarding-error", !!error);
    if (page === 1) {
      title.textContent = "先写一句话，选中要翻译的部分";
      description.textContent = error ?? "不选就翻译整段。";
      primary.textContent = "下一步";
    } else if (page === 2) {
      title.textContent = "按下快捷键，译文就来";
      description.textContent = error ?? "平时选中文字后按这组键，这里先点「翻译」。";
      primary.textContent = busy ? "正在翻译…" : state === "error" ? "重试翻译" : "翻译";
    } else if (done) {
      title.textContent = "译文已放回原处";
      description.textContent = "快捷键和权限在设置里确认后，就能在其他应用里用了。";
      primary.textContent = "设置输入助手";
    } else {
      title.textContent = "点一下，放回原处";
      description.textContent = `${result?.seconds ?? ""} 秒拿到译文，放回去会替换你选中的文字。`;
      primary.textContent = "放回原处";
    }
  }

  function go(next: Page) {
    page = next;
    draw();
  }

  function restart() {
    generation++;
    result = undefined;
    state = "idle";
    page = 1;
    input.value = sample;
    draw();
    input.focus();
    input.select();
  }

  async function showShortcut() {
    let parts = shortcutKeys(win, SPACE, OPTION | SHIFT);
    try {
      const { settings } = await Tolk.getInputAssist();
      parts = shortcutKeys(win, settings.shortcutKeyCode, settings.shortcutModifiers);
    } catch {
      /* the default shortcut still teaches the gesture */
    }
    drawKeys(keys, parts, win);
  }

  async function translate() {
    const original = input.value;
    const text = original.slice(range.start, range.end);
    const mine = ++generation;
    state = "loading";
    draw();
    try {
      const translated = await Tolk.translateOnboarding(text, language());
      if (mine !== generation || !dialog.open) return;
      if (!translated.text.trim()) throw new Error("服务未返回译文");
      result = { text: translated.text, original, seconds: (translated.durationMs / 1000).toFixed(1) };
      $("#onboarding-result").textContent = translated.text;
      $("#onboarding-result-code").textContent = CODES[language()] ?? "";
      state = "idle";
      go(3);
      primary.focus();
    } catch (e) {
      if (mine !== generation || !dialog.open) return;
      state = "error";
      draw(`翻译未完成：${e instanceof Error ? e.message : String(e)}。请检查网络后重试。`);
      primary.focus();
    }
  }

  function replace() {
    if (!result || input.value !== result.original) {
      restart();
      return;
    }
    input.setRangeText(result.text, range.start, range.end, "preserve");
    drawLines(input.value, { start: range.start, end: range.start + result.text.length });
    result = undefined;
    state = "done";
    draw();
    primary.focus();
  }

  async function toSettings() {
    dialog.close();
    try {
      await configure();
    } catch (e) {
      toast(`无法打开输入助手设置：${String(e)}`);
    }
  }

  // Focus moves between pages by script; only keyboard users need to see where it lands.
  dialog.addEventListener("keydown", () => (dialog.dataset.keyboard = "1"));
  dialog.addEventListener("pointerdown", () => delete dialog.dataset.keyboard);
  // Keep the textarea's selected range when a footer button receives a pointer click.
  primary.addEventListener("mousedown", (e) => e.preventDefault());
  primary.addEventListener("click", () => {
    if (page === 1) {
      const whole = input.selectionStart === input.selectionEnd;
      range = whole ? { start: 0, end: input.value.length } : { start: input.selectionStart, end: input.selectionEnd };
      if (!input.value.slice(range.start, range.end).trim()) {
        draw("请先输入或选中要翻译的文字。");
        return;
      }
      drawLines(input.value, range);
      go(2);
      primary.focus();
    } else if (page === 2) void translate();
    else if (state === "done") void toSettings();
    else replace();
  });
  back.addEventListener("click", () => {
    generation++;
    result = undefined;
    state = "idle";
    if (page === 3) go(2);
    else {
      go(1);
      input.focus();
      input.setSelectionRange(range.start, range.end);
    }
  });
  $("#onboarding-replace").addEventListener("click", replace);
  input.addEventListener("input", () => {
    state = "idle";
    draw();
  });
  languages.forEach((l) =>
    l.addEventListener("change", () => {
      generation++;
      state = "idle";
      draw();
    }),
  );
  $("#onboarding-skip").addEventListener("click", () => dialog.close());
  direct.addEventListener("click", () => {
    if (state === "done") restart();
    else void toSettings();
  });
  reopen.hidden = !supported;
  reopen.addEventListener("click", () => {
    document.querySelector<HTMLButtonElement>("#s-cancel")!.click();
    if (!supported || dialog.open) return;
    dialog.showModal();
    restart();
    void showShortcut();
  });
  dialog.addEventListener("close", () => {
    generation++;
    result = undefined;
  });
}
