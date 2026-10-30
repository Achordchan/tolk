import { Tolk, events } from "./mygo";
import { THEMES, applyLook } from "./look";
import { engineList } from "./engines";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

type Cfg = Awaited<ReturnType<typeof Tolk.getSettings>>;
type Prefs = Awaited<ReturnType<typeof Tolk.getPrefs>>;
type Update = Awaited<ReturnType<typeof Tolk.updateStatus>>;

interface Hooks {
  os: string;
  toast: (message: string) => void;
  /** Called after settings were saved, with the fresh prefs. */
  saved: (prefs: Prefs) => void;
  /** Mac-only panes plug in here (permissions, hotkeys, screenshot). */
  mac?: MacPane;
  /** 输入助手, on macOS and Windows. */
  ia?: MacPane;
}

export interface MacPane {
  load(): Promise<void>;
  save(): Promise<void>;
}

const message = (e: unknown) => String(e instanceof Error ? e.message : e);

export function initSettings(h: Hooks) {
  const dlg = $<HTMLDialogElement>("#settings");
  const sEngine = $<HTMLSelectElement>("#s-engine");
  const sVendor = $<HTMLSelectElement>("#s-vendor");
  const sBase = $<HTMLInputElement>("#s-base");
  const sModel = $<HTMLInputElement>("#s-model");
  const sAiKey = $<HTMLInputElement>("#s-ai-key");
  const sDeeplKey = $<HTMLInputElement>("#s-deepl-key");
  const sDc = $<HTMLInputElement>("#s-dc");
  const sDcMs = $<HTMLSelectElement>("#s-dc-ms");
  const sMini = $<HTMLInputElement>("#s-mini");
  const sAuto = $<HTMLInputElement>("#s-autostart");
  const sSrcFont = $<HTMLInputElement>("#s-src-font");
  const sDstFont = $<HTMLInputElement>("#s-dst-font");
  const sError = $("#settings-error");

  let cfg: Cfg;
  let saved: Prefs;
  let draftTheme = "system";
  let autostartWas = false;

  const fill = (sel: HTMLSelectElement, items: [string, string][]) =>
    sel.replaceChildren(...items.map(([value, label]) => new Option(label, value)));

  function syncSections() {
    document.querySelectorAll<HTMLElement>("fieldset[data-for]").forEach((f) => {
      f.hidden = f.dataset.for !== sEngine.value;
    });
  }

  function fillVendor() {
    const p = cfg.profiles?.[sVendor.value];
    const preset = cfg.vendors.find((v) => v.name === sVendor.value);
    sBase.value = p?.baseUrl || preset?.baseUrl || "";
    sModel.value = p?.model || preset?.model || "";
    sAiKey.value = "";
    sAiKey.placeholder = p?.hasKey ? "已保存，留空则不修改" : "";
    $<HTMLDetailsElement>("#ai-adv").open = sVendor.value === "自定义" || !sBase.value;
  }
  sEngine.addEventListener("change", syncSections);

  // ---- shortcuts ----
  const SPEEDS = ["350", "550", "800"];
  const sDcText = $("#s-dc-text");
  sDcText.textContent = h.os === "darwin" ? "选中文字后连按两次 ⌘C，立即翻译" : "选中文字后连按两次 Ctrl+C，立即翻译";
  const syncDc = () => $("#dc-sub").classList.toggle("off", !sDc.checked);
  sDc.addEventListener("change", syncDc);
  function showSpeed(ms: number) {
    // A window set by hand in the old app stays selectable.
    [...sDcMs.options].filter((o) => !SPEEDS.includes(o.value)).forEach((o) => o.remove());
    if (!SPEEDS.includes(String(ms))) sDcMs.append(new Option(`自定义（${ms} 毫秒）`, String(ms)));
    sDcMs.value = String(ms);
  }
  sVendor.addEventListener("change", fillVendor);

  // ---- tabs ----
  let current = "service";
  function show(pane: string) {
    current = pane;
    document.querySelectorAll<HTMLElement>(".tab").forEach((t) => t.classList.toggle("on", t.dataset.pane === pane));
    document.querySelectorAll<HTMLElement>(".pane-body").forEach((p) => (p.hidden = p.dataset.pane !== pane));
    const live = pane === "console" || pane === "about";
    $("#s-save").hidden = live;
    $("#s-cancel").textContent = live ? "关闭" : "取消";
    sError.hidden = true;
    if (pane === "console") startConsole();
    else stopConsole();
  }
  document.querySelectorAll<HTMLElement>(".tab").forEach((t) => t.addEventListener("click", () => show(t.dataset.pane!)));

  // ---- appearance ----
  const themesEl = $("#themes");
  const SWATCH: Record<string, string> = {
    system: "linear-gradient(90deg,#fafafa 50%,#232323 50%)",
    light: "#fafafa",
    dark: "#232323",
    pink: "#fff4f7",
  };
  function drawThemes() {
    themesEl.replaceChildren(
      ...THEMES.map(([id, label]) => {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "theme" + (id === draftTheme ? " on" : "");
        b.innerHTML = `<i style="background:${SWATCH[id]}"></i><span>${label}</span>`;
        b.addEventListener("click", () => {
          draftTheme = id;
          drawThemes();
          preview();
        });
        return b;
      }),
    );
  }
  function preview() {
    $("#o-src-font").textContent = `${sSrcFont.value} px`;
    $("#o-dst-font").textContent = `${sDstFont.value} px`;
    applyLook({ theme: draftTheme, sourceFont: Number(sSrcFont.value), targetFont: Number(sDstFont.value) });
    $("#font-preview").style.fontSize = `${sDstFont.value}px`;
  }
  sSrcFont.addEventListener("input", preview);
  sDstFont.addEventListener("input", preview);

  // ---- console ----
  const list = $("#c-list");
  const level = $<HTMLSelectElement>("#c-level");
  const search = $<HTMLInputElement>("#c-search");
  let lines: Awaited<ReturnType<typeof Tolk.consoleLines>> = [];
  let lastId = 0;
  let poll: number | undefined;
  function drawConsole() {
    const q = search.value.trim().toLowerCase();
    const rows = lines.filter((l) => (level.value === "all" || l.level === level.value) && (!q || l.text.toLowerCase().includes(q)));
    const stick = list.scrollTop + list.clientHeight >= list.scrollHeight - 24;
    list.replaceChildren(
      ...rows.map((l) => {
        const d = document.createElement("div");
        d.className = `c-line ${l.level}`;
        const t = document.createElement("time");
        t.textContent = new Date(l.time).toLocaleTimeString("zh-CN", { hour12: false });
        d.append(t, document.createTextNode(l.text));
        return d;
      }),
    );
    if (stick) list.scrollTop = list.scrollHeight;
  }
  async function pull() {
    try {
      const fresh = await Tolk.consoleLines(lastId);
      if (fresh.length) {
        lastId = fresh[fresh.length - 1]!.id;
        lines = lines.concat(fresh).slice(-2000);
        drawConsole();
      }
    } catch {
      /* the window may be closing */
    }
  }
  function startConsole() {
    void pull();
    poll ??= window.setInterval(pull, 800);
  }
  function stopConsole() {
    clearInterval(poll);
    poll = undefined;
  }
  level.addEventListener("change", drawConsole);
  search.addEventListener("input", drawConsole);
  $("#c-clear").addEventListener("click", async () => {
    await Tolk.clearConsole();
    lines = [];
    drawConsole();
  });
  $("#c-copy").addEventListener("click", async () => {
    const text = Array.from(list.children).map((c) => c.textContent).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      h.toast("已复制当前显示的日志");
    } catch {
      h.toast("复制失败");
    }
  });

  // ---- about + updates ----
  const uBox = $("#a-update");
  const uTitle = $("#u-title");
  const uInstall = $("#u-install");
  const uBar = $("#u-bar");
  const uFill = $("#u-fill");
  const uNotes = $("#u-notes");
  const check = $<HTMLButtonElement>("#a-check");

  function drawUpdate(s: Update) {
    check.disabled = s.state === "checking" || s.state === "downloading" || s.state === "installing";
    uBox.hidden = s.state === "idle" || s.state === "checking";
    uInstall.hidden = s.state !== "available";
    uBar.hidden = s.state !== "downloading";
    uFill.style.width = `${s.percent}%`;
    uNotes.textContent = s.notes ?? "";
    uNotes.hidden = !s.notes;
    uTitle.textContent =
      {
        checking: "正在检查更新…",
        latest: "已是最新版本",
        available: `发现新版本 v${s.version}`,
        downloading: `正在下载 v${s.version}　${s.percent}%`,
        installing: "更新包已校验，正在重启安装…",
        error: `更新失败：${s.error}`,
        idle: "",
      }[s.state] ?? "";
    uTitle.classList.toggle("error", s.state === "error");
    if (s.state === "checking") {
      uBox.hidden = false;
    }
  }
  events.updateChanged.on(drawUpdate);
  check.addEventListener("click", async () => {
    drawUpdate({ state: "checking", version: "", notes: "", force: false, percent: 0, error: "", source: "" });
    try {
      drawUpdate((await Tolk.checkUpdate()) as Update);
    } catch (e) {
      h.toast(message(e));
    }
  });
  uInstall.addEventListener("click", async () => {
    try {
      await Tolk.installUpdate();
    } catch (e) {
      h.toast(message(e));
    }
  });

  // ---- open / save / close ----
  $("#open-settings").addEventListener("click", async () => {
    try {
      cfg = await Tolk.getSettings();
      saved = await Tolk.getPrefs();
      const about = await Tolk.about();
      fill(sEngine, engineList(h.os));
      sEngine.value = cfg.engine;
      fill(sVendor, cfg.vendors.map((v) => [v.name, v.name]));
      sVendor.value = cfg.vendor;
      fillVendor();
      sDeeplKey.value = "";
      sDeeplKey.placeholder = cfg.deeplHasKey ? "已保存，留空则不修改" : "";
      sDc.checked = cfg.prefs.doubleCopy;
      showSpeed(cfg.prefs.doubleCopyMs);
      syncDc();
      sMini.checked = cfg.prefs.miniMode;
      autostartWas = await Tolk.autostart();
      sAuto.checked = autostartWas;
      draftTheme = saved.theme;
      sSrcFont.value = String(saved.sourceFont);
      sDstFont.value = String(saved.targetFont);
      drawThemes();
      preview();
      $("#fs-apple").hidden = h.os !== "darwin";
      const mac = h.os === "darwin";
      $("#tab-mac").hidden = !mac;
      $("#fs-shot").hidden = !mac;
      $("#dc-system-row").hidden = !mac;
      $("#migrate-row").hidden = !mac;
      $("#tab-ia").hidden = !h.ia;
      $("#fs-ia-key").hidden = !h.ia;
      $("#side-version").textContent = `版本 ${about.version}`;
      $("#a-version").textContent = `版本 ${about.version}`;
      $("#a-os").textContent = `${about.os} / ${about.arch}`;
      $("#a-config").textContent = about.configPath;
      $("#a-helper-t").hidden = $("#a-helper").hidden = !about.helper;
      $("#a-helper").textContent = about.helper;
      drawUpdate(await Tolk.updateStatus());
      await h.mac?.load();
      await h.ia?.load();
      syncSections();
      show("service");
      sError.hidden = true;
      dlg.showModal();
    } catch (e) {
      h.toast(message(e));
    }
  });

  const close = () => {
    stopConsole();
    applyLook(saved); // drop an unsaved theme or size preview
    dlg.close();
  };
  $("#s-cancel").addEventListener("click", close);
  dlg.addEventListener("cancel", () => {
    stopConsole();
    applyLook(saved);
  });

  $("#settings-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    if (current === "console" || current === "about") return;
    try {
      await Tolk.saveSettings({
        engine: sEngine.value,
        vendor: sVendor.value,
        baseUrl: sBase.value,
        model: sModel.value,
        aiKey: sAiKey.value,
        deeplKey: sDeeplKey.value,
      });
      await Tolk.setDoubleCopy(sDc.checked, Number(sDcMs.value));
      await Tolk.setMini(sMini.checked);
      if (sAuto.checked !== autostartWas) await Tolk.setAutostart(sAuto.checked);
      await Tolk.setAppearance(draftTheme, Number(sSrcFont.value), Number(sDstFont.value));
      await h.mac?.save();
      await h.ia?.save();
      const next = await Tolk.getPrefs();
      saved = next;
      stopConsole();
      dlg.close();
      h.saved(next);
      h.toast("设置已保存");
    } catch (err) {
      sError.textContent = message(err);
      sError.hidden = false;
    }
  });
}
