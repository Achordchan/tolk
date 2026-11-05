/** Swaps a native <select> for a themed dropdown. The <select> stays in the DOM
 *  (hidden) as the source of truth, so `.value`, `change` and option edits keep working. */
const MAX_MENU = 280;

export function enhanceSelect(sel: HTMLSelectElement) {
  if (sel.dataset.dd) return;
  sel.dataset.dd = "1";

  const wrap = document.createElement("span");
  wrap.className = "dd";
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "dd-btn";
  btn.setAttribute("aria-haspopup", "listbox");
  btn.setAttribute("aria-label", sel.getAttribute("aria-label") ?? "");
  const label = document.createElement("span");
  label.className = "dd-label";
  btn.append(label);
  const menu = document.createElement("div");
  menu.className = "dd-menu";
  menu.setAttribute("role", "listbox");
  menu.hidden = true;
  sel.before(wrap);
  wrap.append(btn, sel);
  sel.hidden = true;
  // The list lives outside the <label> it may sit in (a click on it would be
  // forwarded to the button and reopen it) and is measured against the whole
  // pill, not just the text button inside it.
  const anchor = () => (wrap.closest(".pill") as HTMLElement | null) ?? btn;

  let open = false;
  let active = -1;

  const items = () => [...menu.children] as HTMLElement[];

  function sync() {
    const opt = sel.selectedOptions[0];
    label.textContent = opt?.text ?? "";
    items().forEach((el, i) => {
      const on = sel.options[i]?.value === sel.value;
      el.classList.toggle("on", on);
      el.setAttribute("aria-selected", String(on));
    });
  }

  function build() {
    menu.replaceChildren(
      ...[...sel.options].map((o, i) => {
        const el = document.createElement("div");
        el.className = "dd-item";
        el.setAttribute("role", "option");
        el.textContent = o.text;
        el.addEventListener("mousedown", (e) => e.preventDefault());
        el.addEventListener("click", () => pick(i));
        el.addEventListener("mousemove", () => highlight(i));
        return el;
      }),
    );
    sync();
  }

  function highlight(i: number, scroll = false) {
    active = i;
    items().forEach((el, k) => el.classList.toggle("hot", k === i));
    if (scroll) items()[i]?.scrollIntoView({ block: "nearest" });
  }

  function place() {
    const r = anchor().getBoundingClientRect();
    const room = window.innerHeight - r.bottom - 12;
    const above = r.top - 12;
    const flip = room < 160 && above > room;
    const h = Math.min(MAX_MENU, flip ? above : room);
    menu.style.maxHeight = `${Math.max(120, h)}px`;
    menu.style.minWidth = `${r.width}px`;
    menu.style.left = `${Math.max(8, Math.min(r.left, window.innerWidth - menu.offsetWidth - 8))}px`;
    menu.style.top = flip ? "" : `${r.bottom + 6}px`;
    menu.style.bottom = flip ? `${window.innerHeight - r.top + 6}px` : "";
  }

  function show() {
    if (open || sel.disabled || !sel.options.length) return;
    open = true;
    // A modal <dialog> sits in the top layer, so the list must live inside it.
    (wrap.closest("dialog") ?? document.body).append(menu);
    menu.hidden = false;
    btn.classList.add("open");
    place();
    const i = Math.max(0, sel.selectedIndex);
    highlight(i, true);
    document.addEventListener("mousedown", away, true);
    addEventListener("resize", hide);
    document.addEventListener("scroll", onScroll, true);
  }
  function onScroll(e: Event) {
    if (e.target !== menu) hide(); // scrolling the list itself keeps it open
  }
  function hide() {
    if (!open) return;
    open = false;
    menu.hidden = true;
    btn.classList.remove("open");
    document.removeEventListener("mousedown", away, true);
    removeEventListener("resize", hide);
    document.removeEventListener("scroll", onScroll, true);
  }
  function away(e: Event) {
    const t = e.target as Node;
    if (!wrap.contains(t) && !menu.contains(t)) hide();
  }
  function pick(i: number) {
    const changed = sel.selectedIndex !== i;
    sel.selectedIndex = i;
    sync();
    hide();
    btn.focus();
    if (changed) sel.dispatchEvent(new Event("change", { bubbles: true }));
  }

  btn.addEventListener("click", () => (open ? hide() : show()));
  btn.addEventListener("keydown", (e) => {
    const n = sel.options.length;
    if (e.key === "Escape" && open) {
      e.preventDefault();
      e.stopPropagation();
      hide();
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!open) return show();
      highlight((active + (e.key === "ArrowDown" ? 1 : n - 1)) % n, true);
    } else if ((e.key === "Enter" || e.key === " ") && open) {
      e.preventDefault();
      if (active >= 0) pick(active);
    } else if (e.key === "Tab") {
      hide();
    }
  });

  // Programmatic `.value = x` must refresh the label too.
  const proto = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")!;
  Object.defineProperty(sel, "value", {
    get() {
      return proto.get!.call(sel);
    },
    set(v: string) {
      proto.set!.call(sel, v);
      sync();
    },
  });
  new MutationObserver(build).observe(sel, { childList: true });
  sel.addEventListener("change", sync);
  build();
}
