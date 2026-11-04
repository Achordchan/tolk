import { Tolk } from "./mygo";

/** Uses the production translator; only the editable starting sentence is illustrative. */
export function initOnboarding(os: string, configure: () => Promise<void>, toast: (message: string) => void) {
  const supported = os === "windows" || os === "darwin";
  const dialog = document.querySelector<HTMLDialogElement>("#onboarding")!;
  const $ = <T extends HTMLElement>(id: string) => dialog.querySelector<T>(id)!;
  const input = $<HTMLTextAreaElement>("#onboarding-input");
  const language = $<HTMLSelectElement>("#onboarding-language");
  const test = $<HTMLButtonElement>("#onboarding-test");
  const candidate = $("#onboarding-candidate");
  const status = $("#onboarding-status");
  const reset = $<HTMLButtonElement>("#onboarding-reset");
  const reopen = document.querySelector<HTMLButtonElement>("#open-onboarding")!;
  const sample = input.value;
  let generation = 0;
  let result: { text: string; original: string; start: number; end: number } | undefined;
  function stage(value: "idle" | "loading" | "candidate" | "done" | "error") {
    dialog.dataset.state = value;
    const busy = value === "loading";
    input.readOnly = busy;
    language.disabled = busy;
    test.disabled = busy || !input.value.trim();
    reset.disabled = busy;
    test.textContent = busy ? "正在翻译…" : value === "error" ? "重试翻译" : "选中并翻译";
    candidate.hidden = value !== "candidate";
    const current = value === "done" ? 2 : value === "candidate" || busy ? 1 : 0;
    dialog.querySelectorAll(".onboarding-steps li").forEach((el, i) => {
      if (i === current) el.setAttribute("aria-current", "step"); else el.removeAttribute("aria-current");
    });
  }
  function invalidate() { generation++; result = undefined; stage("idle"); status.textContent = "选中部分内容，或直接翻译整段。"; }
  function open() {
    if (!supported || dialog.open) return;
    input.value = sample;
    invalidate();
    dialog.showModal();
    test.focus();
  }
  // Keep the textarea's selected range when the action button receives a pointer click.
  test.addEventListener("mousedown", e => e.preventDefault());
  test.addEventListener("click", async () => {
    const original = input.value;
    const start = input.selectionStart === input.selectionEnd ? 0 : input.selectionStart;
    const end = input.selectionStart === input.selectionEnd ? original.length : input.selectionEnd;
    const text = original.slice(start, end);
    if (!text.trim()) { status.textContent = "请先输入或选中要翻译的文字。"; return; }
    const mine = ++generation;
    input.focus(); input.setSelectionRange(start, end);
    stage("loading");
    status.textContent = "正在使用微软翻译，请稍候…";
    try {
      const translated = await Tolk.translateOnboarding(text, language.value);
      if (mine !== generation || !dialog.open) return;
      if (!translated.text.trim()) throw new Error("服务未返回译文");
      result = { text: translated.text, original, start, end };
      $("#onboarding-result").textContent = translated.text;
      $("#onboarding-result-label").textContent = `${language.selectedOptions[0]!.text}候选`;
      $("#onboarding-result-meta").textContent = `${(translated.durationMs / 1000).toFixed(1)} 秒`;
      stage("candidate");
      status.textContent = "译文已返回。点击候选，将它放回原来的位置。";
      $("#onboarding-replace").focus();
    } catch (e) {
      if (mine !== generation || !dialog.open) return;
      stage("error");
      status.textContent = `翻译未完成：${e instanceof Error ? e.message : String(e)}。请检查网络后重试。`;
    }
  });
  $("#onboarding-replace").addEventListener("click", () => {
    if (!result || input.value !== result.original) { invalidate(); return; }
    input.setRangeText(result.text, result.start, result.end, "select");
    stage("done");
    input.focus();
    status.textContent = "已替换。接下来设置输入助手，在聊天、邮件和文档中使用。";
    result = undefined;
  });
  input.addEventListener("input", invalidate);
  language.addEventListener("change", invalidate);
  reset.addEventListener("click", () => { input.value = sample; invalidate(); input.focus(); });
  reopen.hidden = !supported;
  reopen.addEventListener("click", () => { document.querySelector<HTMLButtonElement>("#s-cancel")!.click(); open(); });
  $("#onboarding-next").addEventListener("click", async () => {
    dialog.close();
    try { await configure(); } catch (e) { toast(`无法打开输入助手设置：${String(e)}`); }
  });
  $("#onboarding-skip").addEventListener("click", () => { dialog.close(); });
  dialog.addEventListener("close", () => { generation++; result = undefined; });
}
