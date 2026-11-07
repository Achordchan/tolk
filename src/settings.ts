import { initPluginPanel } from "./plugin";
import { Tolk, events } from "./mygo";
import { THEMES, applyLook } from "./look";
import { engineList } from "./engines";
import { openUpdate } from "./update";
import { initFeedback, openFeedback } from "./feedback";
import { drawKeys, playDouble, pressKeys } from "./keys";

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
  /** Keep the pane's live status current while the sheet is open; called once it is. */
  watch?(): void;
  save(): Promise<void>;
  /** Draft state no single form control holds, for spotting unsaved changes. */
  draft?(): string;
}

const message = (e: unknown) => String(e instanceof Error ? e.message : e);

/** One line each, so the services can be compared without opening them. */
const ENGINE_HINTS: Record<string, string> = {
  apple: "本机离线，不用密钥",
  google: "不用密钥，需能访问 Google",
  microsoft: "不用密钥，需能访问 bing.com",
  deepl: "需要 API Key，可免费申请",
  openai_compat: "自选模型，需要 API Key",
  achord_builtin: "独立插件，不用密钥",
};

/** "Tolk" in the languages it works between; the app icon on 关于 walks through them. */
const WORDS = ["Tolk 在荷兰语里是「口译员」", "英语叫 interpreter", "日语叫 通訳", "德语叫 Dolmetscher", "法语叫 interprète", "韩语叫 통역사", "西班牙语叫 intérprete"];

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
  $("#p-shot-windows").addEventListener("click", async () => {
    $("#s-cancel").click();
    try { await Tolk.screenshotWindows(); } catch (e) { h.toast(message(e)); }
  });

  let cfg: Cfg;
  let saved: Prefs;
  let draftTheme = "system";
  let autostartWas = false;

  const fill = (sel: HTMLSelectElement, items: [string, string][]) =>
    sel.replaceChildren(...items.map(([value, label]) => new Option(label, value)));

  const refreshPlugin = initPluginPanel();

  function syncSections() {
    document.querySelectorAll<HTMLElement>("fieldset[data-for]").forEach((f) => {
      f.hidden = f.dataset.for !== sEngine.value;
    });
    void refreshPlugin();
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

  // The services, side by side. The hidden <select> stays the source of truth.
  const enginesEl = $("#engines");
  function drawEngines() {
    enginesEl.replaceChildren(
      ...[...sEngine.options].map((o) => {
        const l = document.createElement("label");
        l.className = "engine";
        const radio = document.createElement("input");
        radio.type = "radio";
        radio.name = "engine";
        radio.value = o.value;
        radio.checked = o.value === sEngine.value;
        radio.addEventListener("change", () => {
          sEngine.value = o.value;
          sEngine.dispatchEvent(new Event("change", { bubbles: true }));
          // In a short window the service's fields can open below the fold.
          document.querySelector(`fieldset[data-for="${o.value}"]`)?.scrollIntoView({ block: "nearest", behavior: "smooth" });
        });
        const hint = document.createElement("small");
        hint.textContent = ENGINE_HINTS[o.value] ?? "";
        l.append(radio, o.text, hint);
        l.insertAdjacentHTML("beforeend", '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12.5 4.5 4.5L19 7.5"/></svg>');
        return l;
      }),
    );
  }

  // ---- shortcuts ----
  const SPEEDS = ["350", "550", "800"];
  const sDcText = $("#s-dc-text");
  sDcText.textContent = h.os === "darwin" ? "选中文字后连按两次 ⌘C，立即翻译" : "选中文字后连按两次 Ctrl+C，立即翻译";
  const syncDc = () => {
    $("#dc-sub").classList.toggle("off", !sDc.checked);
    drawTempo();
  };
  sDc.addEventListener("change", syncDc);

  // The stage on 快捷键: real caps that press at the chosen speed, and that answer
  // when the gesture is tried right here.
  const dcKeys = $("#dc-keys");
  const dcNote = $("#dc-note");
  const mac = h.os === "darwin";
  drawKeys(dcKeys, [mac ? "⌘" : "Ctrl", "C"], !mac);
  let lastTap = 0;
  let tapTimer: number | undefined;
  function drawTempo() {
    lastTap = 0;
    clearTimeout(tapTimer);
    dcNote.className = "stage-note";
    dcNote.textContent = sDc.checked ? `两下要在 ${sDcMs.value} 毫秒内。在这里连按两次试试手速` : "连按两次复制翻译已关闭";
  }
  function showTap(gap: number) {
    const limit = Number(sDcMs.value);
    const hit = gap <= limit;
    dcNote.className = `stage-note ${hit ? "hit" : "miss"}`;
    dcNote.textContent = hit
      ? `隔了 ${gap} 毫秒，在 ${limit} 毫秒内，能触发翻译`
      : `隔了 ${gap} 毫秒，超过 ${limit} 毫秒，不会触发。可以把连按速度调慢一点`;
  }
  sDcMs.addEventListener("change", () => {
    drawTempo();
    if (sDc.checked) playDouble(dcKeys, Number(sDcMs.value));
  });
  dlg.addEventListener("keydown", (e) => {
    if (current !== "keys" || !sDc.checked || e.repeat) return;
    if (!(mac ? e.metaKey : e.ctrlKey) || e.key.toLowerCase() !== "c") return;
    pressKeys(dcKeys);
    const now = performance.now();
    clearTimeout(tapTimer);
    if (lastTap && now - lastTap < 2500) {
      showTap(Math.round(now - lastTap));
      lastTap = 0;
      return;
    }
    lastTap = now;
    dcNote.className = "stage-note";
    dcNote.textContent = "再按一下…";
    tapTimer = window.setTimeout(drawTempo, 2500);
  });
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
    const again = pane === current;
    current = pane;
    document.querySelectorAll<HTMLElement>(".tab").forEach((t) => t.classList.toggle("on", t.dataset.pane === pane));
    document.querySelectorAll<HTMLElement>(".pane-body").forEach((p) => (p.hidden = p.dataset.pane !== pane));
    drawFooter();
    sError.hidden = true;
    moveSlider(dlg.open);
    if (pane === "console") startConsole();
    else stopConsole();
    if (pane === "keys" && !again && sDc.checked && dlg.open) playDouble(dcKeys, Number(sDcMs.value), 180);
  }
  document.querySelectorAll<HTMLElement>(".tab").forEach((t) => t.addEventListener("click", () => show(t.dataset.pane!)));

  // One chip slides between the tabs. It needs the sheet on screen to measure,
  // so opening puts it in place without the slide.
  const slider = $("#tab-slider");
  function moveSlider(animate: boolean) {
    const tab = document.querySelector<HTMLElement>(".tab.on");
    if (!tab || !dlg.open) return;
    slider.classList.toggle("still", !animate);
    slider.style.transform = `translateY(${tab.offsetTop}px)`;
    slider.style.height = `${tab.offsetHeight}px`;
  }
  addEventListener("resize", () => moveSlider(false));

  // Nothing is written until 保存, so the footer says when there is something to
  // lose: the draft compared with what the sheet opened with, so undoing a change
  // by hand clears it again.
  const LIVE = ["c-level", "c-search", "a-autoreport"];
  let dirty = false;
  let baseline: Record<string, string> = {};
  function readDraft() {
    const draft: Record<string, string> = { theme: draftTheme, ia: h.ia?.draft?.() ?? "" };
    $("#settings-form")
      .querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>("input[id], select[id], textarea[id]")
      .forEach((c) => {
        if (LIVE.includes(c.id)) return;
        draft[c.id] = c instanceof HTMLInputElement && c.type === "checkbox" ? String(c.checked) : c.value;
      });
    return draft;
  }
  function checkDirty() {
    const draft = readDraft();
    dirty = Object.keys(draft).some((k) => draft[k] !== baseline[k]);
    drawFooter();
  }
  /** Take the draft as it stands as the saved state. */
  function resetDirty() {
    baseline = readDraft();
    checkDirty();
  }
  /** Changed from the menu bar or tray: that value is saved already, not a draft. */
  function syncSaved(id: string, value: boolean) {
    baseline[id] = String(value);
    if (dlg.open) checkDirty();
  }
  events.inputAssistChanged.on((on) => syncSaved("ia-enabled", on));
  const save = $("#s-save");
  /** These panes have nothing of their own to save. */
  const nothingToSave = () => ["console", "about", "mac"].includes(current);
  function drawFooter() {
    save.hidden = nothingToSave() && !dirty;
    save.className = dirty ? "primary" : "ghost";
    $("#s-cancel").textContent = dirty ? "取消" : "关闭";
    $("#settings-dirty").hidden = !dirty;
  }
  for (const type of ["input", "change"]) $("#settings-form").addEventListener(type, checkDirty);

  // ---- appearance ----
  const themesEl = $("#themes");
  // [paper, ink, stage, accent]: each tile is a small window in its own colours.
  const SWATCH: Record<string, [string, string, string, string]> = {
    system: ["#fafafa", "#1d1d1f", "#29313a", "#0a84ff"],
    light: ["#fafafa", "#1d1d1f", "#dce5ed", "#0a84ff"],
    dark: ["#232323", "#ececee", "#29313a", "#0a84ff"],
    pink: ["#fffbfc", "#4a3039", "#ffeaf2", "#ff69b4"],
  };
  function drawThemes() {
    themesEl.replaceChildren(
      ...THEMES.map(([id, label]) => {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "theme" + (id === draftTheme ? " on" : "");
        b.setAttribute("aria-pressed", String(id === draftTheme));
        const [paper, ink, stage, accent] = SWATCH[id]!;
        b.innerHTML = `<i style="background:${paper};color:${ink};--tile-stage:${stage};--tile-accent:${accent}"><b></b><b></b></i><span>${label}</span>`;
        b.addEventListener("click", () => {
          draftTheme = id;
          drawThemes();
          preview();
          checkDirty();
        });
        return b;
      }),
    );
  }
  function preview() {
    $("#o-src-font").textContent = `${sSrcFont.value} px`;
    $("#o-dst-font").textContent = `${sDstFont.value} px`;
    for (const range of [sSrcFont, sDstFont]) {
      const fill = ((Number(range.value) - Number(range.min)) / (Number(range.max) - Number(range.min))) * 100;
      range.style.setProperty("--fill", `${fill}%`);
    }
    applyLook({ theme: draftTheme, sourceFont: Number(sSrcFont.value), targetFont: Number(sDstFont.value) });
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
  search.addEventListener("keydown", (e) => {
    if (e.key === "Enter") e.preventDefault(); // searching the log never saves the sheet
  });
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
  const aStatus = $("#a-status");
  const check = $<HTMLButtonElement>("#a-check");

  // The dialog in update.ts does the talking; 关于 only says where things stand.
  function drawUpdate(s: Update) {
    check.disabled = s.state === "checking" || s.state === "installing";
    const found = ["available", "downloading", "ready"].includes(s.state);
    check.textContent = found ? "查看更新" : "检查更新";
    aStatus.textContent =
      {
        checking: "正在检查更新…",
        latest: "已是最新版本",
        available: `发现新版本 v${s.version}，准备下载`,
        downloading: `正在后台下载 v${s.version}　${s.percent}%`,
        ready: `v${s.version} 已下载好，重启或退出 Tolk 时安装`,
        installing: "正在重启安装…",
        error: s.version ? `下载 v${s.version} 失败：${s.error}` : `检查更新失败：${s.error}`,
      }[s.state] ?? "";
    aStatus.hidden = !aStatus.textContent;
    aStatus.classList.toggle("error", s.state === "error");
    aStatus.classList.toggle("muted", s.state !== "error");
  }
  events.updateChanged.on(drawUpdate);
  // Mini mode switched from the menu bar / tray while the sheet is open.
  events.prefsChanged.on((p) => {
    if (!dlg.open) return;
    sMini.checked = p.miniMode;
    syncSaved("s-mini", p.miniMode);
  });

  // The icon on 关于 says the app's name in another language at each click.
  let word = 0;
  $("#a-mark").addEventListener("click", () => {
    word = (word + 1) % WORDS.length;
    const el = $("#a-word");
    el.textContent = WORDS[word]!;
    el.classList.remove("land");
    void el.offsetWidth;
    el.classList.add("land");
  });

  // ---- 提交问题 ----
  initFeedback();
  const autoReport = $<HTMLInputElement>("#a-autoreport");
  $("#a-feedback").addEventListener("click", () => openFeedback());
  $("#c-feedback").addEventListener("click", (e) => {
    e.preventDefault();
    openFeedback();
  });
  // Takes effect at once, like the rest of 关于.
  autoReport.addEventListener("change", async () => {
    try {
      await Tolk.setAutoReport(autoReport.checked);
    } catch (e) {
      autoReport.checked = !autoReport.checked;
      h.toast(message(e));
    }
  });
  check.addEventListener("click", async () => {
    if (["available", "downloading", "ready"].includes((await Tolk.updateStatus()).state)) return openUpdate();
    try {
      const s = (await Tolk.checkUpdate()) as Update;
      if (["available", "downloading", "ready"].includes(s.state)) openUpdate();
    } catch (e) {
      h.toast(message(e));
    }
  });

  // ---- open / save / close ----
  async function open(pane = "service") {
    if (dlg.open) return show(pane);
    try {
      // Asked all at once: the sheet waits for the slowest answer, not their sum.
      const [settings, prefs, about, autostart, update, feedback] = await Promise.all([
        Tolk.getSettings(),
        Tolk.getPrefs(),
        Tolk.about(),
        Tolk.autostart(),
        Tolk.updateStatus(),
        Tolk.feedbackInfo(),
        h.mac?.load(),
        h.ia?.load(),
      ]);
      cfg = settings;
      saved = prefs;
      fill(sEngine, engineList(h.os));
      sEngine.value = cfg.engine;
      drawEngines();
      fill(sVendor, cfg.vendors.map((v) => [v.name, v.name]));
      sVendor.value = cfg.vendor;
      fillVendor();
      sDeeplKey.value = "";
      sDeeplKey.placeholder = cfg.deeplHasKey ? "已保存，留空则不修改" : "";
      sDc.checked = cfg.prefs.doubleCopy;
      showSpeed(cfg.prefs.doubleCopyMs);
      syncDc();
      sMini.checked = cfg.prefs.miniMode;
      autostartWas = autostart;
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
      $("#fs-shot-windows").hidden = h.os !== "windows";
      $<HTMLInputElement>("#s-win-shot").checked = saved.screenshotHotkey;
      $<HTMLInputElement>("#s-win-shot-auto").checked = saved.screenshotAutoTranslate;
      $<HTMLInputElement>("#s-mac-shot-auto").checked = saved.screenshotAutoTranslate;
      $("#dc-system-row").hidden = !mac;
      $("#tab-ia").hidden = !h.ia;
      $("#fs-ia-key").hidden = !h.ia;
      $("#a-version").textContent = `版本 ${about.version}`;
      $("#a-os").textContent = `${about.os} / ${about.arch}`;
      $("#a-config").textContent = about.configPath;
      $("#a-helper-t").hidden = $("#a-helper").hidden = !about.helper;
      $("#a-helper").textContent = about.helper;
      drawUpdate(update);
      autoReport.checked = feedback.autoReport;
      syncSections();
      resetDirty();
      current = pane;
      show(pane);
      sError.hidden = true;
      dlg.showModal();
      h.mac?.watch?.();
      h.ia?.watch?.();
      // Keyboard focus starts on the sheet itself, not outlined on the first tab.
      dlg.focus();
      moveSlider(false);
      if (pane === "keys" && sDc.checked) playDouble(dcKeys, Number(sDcMs.value), 240);
    } catch (e) {
      h.toast(message(e));
    }
  }
  $("#open-settings").addEventListener("click", () => void open());

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
    if (nothingToSave() && !dirty) return;
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
      if (h.os === "windows") await Tolk.setWindowsScreenshotHotkey($<HTMLInputElement>("#s-win-shot").checked);
      if (h.os === "windows" || mac) {
        const autoShot = $<HTMLInputElement>(mac ? "#s-mac-shot-auto" : "#s-win-shot-auto").checked;
        if (autoShot !== saved.screenshotAutoTranslate) await Tolk.setScreenshotAutoTranslate(autoShot);
      }
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

  return { open };
}
