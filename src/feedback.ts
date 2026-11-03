import { Tolk } from "./mygo";

// 提交问题: the user describes the problem; Go adds version, system and the
// (scrubbed) logs and sends it to the relay that files the GitHub issue. When
// the relay cannot be reached, the full report is on the clipboard and
// GitHub's new-issue page opens filled in.

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;
const message = (e: unknown) => String(e instanceof Error ? e.message : e);

let dlg: HTMLDialogElement;
let busy = false;
let fallback = "";

function reset() {
  busy = false;
  fallback = "";
  $<HTMLTextAreaElement>("#fb-desc").disabled = false;
  $<HTMLInputElement>("#fb-contact").disabled = false;
  $<HTMLInputElement>("#fb-logs").disabled = false;
  $("#fb-error").hidden = true;
  $("#fb-done").hidden = true;
  $("#fb-github").hidden = true;
  const send = $<HTMLButtonElement>("#fb-send");
  send.hidden = false;
  send.disabled = false;
  send.textContent = "提交";
  $("#fb-cancel").textContent = "取消";
  $<HTMLDetailsElement>("#fb-preview-box").open = false;
}

function fields() {
  return [
    $<HTMLTextAreaElement>("#fb-desc").value,
    $<HTMLInputElement>("#fb-contact").value,
    $<HTMLInputElement>("#fb-logs").checked,
  ] as const;
}

async function refreshPreview() {
  const box = $<HTMLDetailsElement>("#fb-preview-box");
  if (!box.open) return;
  try {
    $("#fb-preview").textContent = await Tolk.feedbackPreview(...fields());
  } catch (e) {
    $("#fb-preview").textContent = message(e);
  }
}

function showError(text: string) {
  const el = $("#fb-error");
  el.textContent = text;
  el.hidden = !text;
}

function showDone(url: string) {
  const done = $("#fb-done");
  done.replaceChildren("已提交，谢谢！开发者会在 ");
  const a = document.createElement("a");
  a.href = url;
  a.textContent = "这个问题单";
  a.addEventListener("click", (e) => {
    e.preventDefault();
    void Tolk.openLink(url);
  });
  done.append(a, " 里回复，可以收藏它关注进展。");
  done.hidden = false;
}

async function submit() {
  if (busy) return;
  const [desc] = fields();
  if (!desc.trim()) {
    showError("请先描述一下遇到的问题");
    $<HTMLTextAreaElement>("#fb-desc").focus();
    return;
  }
  busy = true;
  showError("");
  const send = $<HTMLButtonElement>("#fb-send");
  const cancel = $<HTMLButtonElement>("#fb-cancel");
  send.disabled = true;
  send.textContent = "正在提交…";
  // Closing now would leave the request finishing unseen, and a reopened
  // dialog could send a second one and get the first one's answer.
  cancel.disabled = true;
  try {
    const r = await Tolk.submitFeedback(...fields());
    if (r.sent) {
      $<HTMLTextAreaElement>("#fb-desc").value = "";
      $<HTMLTextAreaElement>("#fb-desc").disabled = true;
      $<HTMLInputElement>("#fb-contact").disabled = true;
      $<HTMLInputElement>("#fb-logs").disabled = true;
      send.hidden = true;
      $("#fb-cancel").textContent = "完成";
      showDone(r.url);
      return;
    }
    if (r.issueUrl) {
      fallback = r.issueUrl;
      showError(`暂时连不上提交服务（${r.error}）。完整内容已复制到剪贴板，可以改用 GitHub 提交：打开后如果日志不全，把剪贴板内容粘贴进去即可。`);
      $("#fb-github").hidden = false;
    } else {
      showError(r.error || "提交失败");
    }
  } catch (e) {
    showError(message(e));
  } finally {
    busy = false;
    cancel.disabled = false;
    if (!send.hidden) {
      send.disabled = false;
      send.textContent = "重试";
    }
  }
}

/** Opens the dialog; text pre-fills the description. */
export function openFeedback(text = "") {
  reset();
  if (text) $<HTMLTextAreaElement>("#fb-desc").value = text;
  if (!dlg.open) dlg.showModal();
  $<HTMLTextAreaElement>("#fb-desc").focus();
}

export function initFeedback() {
  dlg = $<HTMLDialogElement>("#feedback-dialog");
  $("#fb-send").addEventListener("click", submit);
  $("#fb-cancel").addEventListener("click", () => {
    if (!busy) dlg.close();
  });
  $("#fb-github").addEventListener("click", () => {
    if (fallback) void Tolk.openLink(fallback);
  });
  $("#fb-preview-box").addEventListener("toggle", refreshPreview);
  $("#fb-logs").addEventListener("change", refreshPreview);
  // A request in flight would finish unseen; keep the dialog until it answers.
  dlg.addEventListener("cancel", (e) => {
    if (busy) e.preventDefault();
  });
}
