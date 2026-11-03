import { Tolk, events } from "./mygo";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

type Update = Awaited<ReturnType<typeof Tolk.updateStatus>>;

const message = (e: unknown) => String(e instanceof Error ? e.message : e);

let dlg: HTMLDialogElement;
let last: Update = { state: "idle", version: "", notes: "", force: false, percent: 0, error: "", source: "" };
// The version the user answered 稍后 to: not asked again until the next launch
// (it installs when Tolk quits).
let dismissed = "";
// The version the user asked to install while it was still downloading: it
// restarts by itself once the download is done.
let queued = "";

/** The changelog section as a list; its headings repeat what the dialog already says. */
function drawNotes(notes: string) {
  const list = $("#ud-notes");
  const lines = notes.split("\n").map((l) => l.trim()).filter((l) => l && !l.startsWith("#"));
  list.replaceChildren(
    ...lines.map((l) => {
      const li = document.createElement("li");
      li.textContent = l.replace(/^[-*]\s+/, "");
      return li;
    }),
  );
  list.hidden = !lines.length;
}

function draw(s: Update) {
  $("#ud-title").textContent =
    {
      available: `发现新版本 v${s.version}`,
      downloading: `正在下载 v${s.version}`,
      ready: `新版本 v${s.version} 已下载好`,
      installing: "正在重启安装…",
      error: "更新失败",
    }[s.state] ?? "";
  drawNotes(["available", "downloading", "ready"].includes(s.state) ? s.notes : "");
  $("#ud-hint").hidden = s.state !== "ready" || s.force;
  const err = $("#ud-error");
  err.textContent = s.state === "error" ? s.error : "";
  err.hidden = s.state !== "error";
  $("#ud-progress").hidden = s.state !== "downloading";
  $("#ud-fill").style.width = `${s.percent}%`;
  $("#ud-percent").textContent = `${s.percent}%`;
  // A download goes on in the background after the dialog closes; only a
  // forced update or the restart itself cannot be waved away.
  $("#ud-later").hidden = s.state === "installing" || (s.force && s.state !== "error");
  $("#ud-later").textContent = s.state === "error" || s.state === "downloading" ? "关闭" : "稍后";
  // While downloading, 立即更新 asks for a restart as soon as it is done.
  const install = $<HTMLButtonElement>("#ud-install");
  const waiting = s.state === "downloading" && queued === s.version;
  install.hidden = s.state === "installing" || (s.state === "downloading" && s.force); // a forced one restarts anyway
  install.disabled = waiting;
  install.textContent = waiting
    ? "下载完自动重启"
    : ({ ready: "重启更新", error: "重试", downloading: "下载完立即更新" }[s.state] ?? "立即更新");
}

/** Show the dialog for the update found last, whatever the user answered before. */
export function openUpdate() {
  draw(last);
  if (!dlg.open) dlg.showModal();
}

/**
 * A new version downloads by itself; the dialog asks once it is ready, so
 * nobody has to look for it in 关于. Background progress and failures only show
 * in 关于, unless the dialog is already open.
 */
export async function initUpdate(toast: (m: string) => void) {
  dlg = $<HTMLDialogElement>("#update-dialog");
  const apply = (s: Update) => {
    // A later silent check must not disturb a dialog that is still asking.
    const background = s.state === "checking" || s.state === "latest" || s.state === "idle";
    if (dlg.open && background) return;
    // A failed download drops the restart the user asked for (the backend
    // forgets it too).
    if (s.state === "error") queued = "";
    last = s;
    if (s.state === "ready" && s.version !== dismissed) openUpdate();
    else if (dlg.open) draw(s);
    else if (s.force && (s.state === "downloading" || s.state === "installing")) openUpdate();
  };
  // 稍后 on a downloaded update: do not ask again this run. Closing while it
  // downloads still lets it ask once it is ready.
  const dismiss = () => {
    if (last.state !== "downloading") dismissed = last.version;
  };
  $("#ud-later").addEventListener("click", () => {
    dismiss();
    dlg.close();
  });
  $("#ud-install").addEventListener("click", async () => {
    try {
      await Tolk.installUpdate();
      if (last.state === "downloading") {
        queued = last.version;
        draw(last);
      }
    } catch (e) {
      draw({ ...last, state: "error", error: message(e) });
    }
  });
  // Esc means 稍后, except where the button is hidden.
  dlg.addEventListener("cancel", (e) => {
    if ($("#ud-later").hidden) e.preventDefault();
    else dismiss();
  });
  events.updateChanged.on(apply);
  apply(await Tolk.updateStatus());
  const notice = await Tolk.updateNotice();
  if (notice) toast(notice);
}
