import { Tolk, events } from "./mygo";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

type Update = Awaited<ReturnType<typeof Tolk.updateStatus>>;

const message = (e: unknown) => String(e instanceof Error ? e.message : e);

let dlg: HTMLDialogElement;
let last: Update = { state: "idle", version: "", notes: "", force: false, percent: 0, error: "", source: "" };
// The version the user answered 稍后 to: not asked again until the next launch.
let dismissed = "";

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
  const busy = s.state === "downloading" || s.state === "installing";
  $("#ud-title").textContent =
    {
      available: `发现新版本 v${s.version}`,
      downloading: `正在下载 v${s.version}`,
      installing: "正在重启安装…",
      error: "更新失败",
    }[s.state] ?? "";
  drawNotes(s.state === "available" || s.state === "downloading" ? s.notes : "");
  const err = $("#ud-error");
  err.textContent = s.state === "error" ? s.error : "";
  err.hidden = s.state !== "error";
  $("#ud-progress").hidden = s.state !== "downloading";
  $("#ud-fill").style.width = `${s.percent}%`;
  $("#ud-percent").textContent = `${s.percent}%`;
  $("#ud-later").hidden = busy || (s.state === "available" && s.force);
  $("#ud-later").textContent = s.state === "error" ? "关闭" : "稍后";
  $("#ud-install").hidden = busy;
  $("#ud-install").textContent = s.state === "error" ? "重试" : "立即更新";
}

/** Show the dialog for the update found last, whatever the user answered before. */
export function openUpdate() {
  draw(last);
  if (!dlg.open) dlg.showModal();
}

/** Asks as soon as a new version is known, so nobody has to look for it in 关于. */
export async function initUpdate() {
  dlg = $<HTMLDialogElement>("#update-dialog");
  const apply = (s: Update) => {
    // A later silent check, or its failure, must not disturb a dialog that is still asking.
    const background = s.state === "checking" || s.state === "latest" || s.state === "idle" || (s.state === "error" && !s.version);
    if (dlg.open && background) return;
    last = s;
    if (s.state === "available" && s.version !== dismissed) openUpdate();
    else if (dlg.open) draw(s);
    else if (s.force && (s.state === "downloading" || s.state === "installing")) openUpdate();
  };
  $("#ud-later").addEventListener("click", () => {
    dismissed = last.version;
    dlg.close();
  });
  $("#ud-install").addEventListener("click", async () => {
    try {
      await Tolk.installUpdate();
    } catch (e) {
      draw({ ...last, state: "error", error: message(e) });
    }
  });
  // Esc means 稍后, but a forced update or a running download cannot be waved away.
  dlg.addEventListener("cancel", (e) => {
    if ($("#ud-later").hidden) e.preventDefault();
    else dismissed = last.version;
  });
  events.updateChanged.on(apply);
  apply(await Tolk.updateStatus());
}
