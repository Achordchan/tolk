import { Tolk, events } from "./mygo";

const $ = <T extends HTMLElement>(selector: string) => document.querySelector<T>(selector)!;

$("#version").textContent = await Tolk.version();

const cfg = await Tolk.legacyConfig();
$("#legacy").textContent = cfg.found
  ? `Found ${cfg.path} · engine ${cfg.engine} · keys: ${(cfg.keyVendors ?? []).join(", ") || "none"}`
  : "No dazuo-win config found";

events.copied.on((text) => {
  $("#clip").textContent = text || "(clipboard empty)";
});
