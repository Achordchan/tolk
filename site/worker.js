// tolk.achord.cn: the landing page plus download redirects that always point
// at the newest GitHub release, so publishing a version needs no site change.
import html from "./index.html";
import icon from "./icon.svg";

const REPO = "https://github.com/Achordchan/tolk";
const LATEST_JSON = REPO + "/releases/latest/download/latest.json";
// Same public proxies the app's updater uses (internal/update/release.go).
const MIRRORS = { mirror: "https://ghfast.top/", mirror2: "https://gh-proxy.com/" };

// Used when GitHub cannot be reached; replaced by latest.json on every fetch.
const FALLBACK = {
  version: "2.2.3",
  win: { name: "Tolk-setup-windows_2.2.3.exe", size: 11289567, url: REPO + "/releases/download/v2.2.3/Tolk-setup-windows_2.2.3.exe" },
  mac: { name: "Tolk-2.2.3-macos.dmg", size: 13832464, url: REPO + "/releases/download/v2.2.3/Tolk-2.2.3-macos.dmg" },
};

// The parsed result is cached, not GitHub's response: latest.json answers with
// a redirect to a signed link that expires, which must not be replayed later.
const CACHE_KEY = "https://tolk.achord.cn/__latest";

async function latest(ctx) {
  const cache = caches.default;
  const hit = await cache.match(CACHE_KEY);
  if (hit) return hit.json();
  const d = await fetchLatest();
  if (d !== FALLBACK) {
    const res = new Response(JSON.stringify(d), { headers: { "Content-Type": "application/json", "Cache-Control": "max-age=600" } });
    ctx.waitUntil(cache.put(CACHE_KEY, res));
  }
  return d;
}

async function fetchLatest() {
  try {
    const r = await fetch(LATEST_JSON, { headers: { "User-Agent": "tolk-site" } });
    if (!r.ok) throw new Error("latest.json " + r.status);
    const d = await r.json();
    const pick = (re) => {
      const a = (d.assets || []).find((x) => re.test(x.name));
      return a && { name: a.name, size: a.size, url: a.browser_download_url };
    };
    const win = pick(/^Tolk-setup-windows_.*\.exe$/);
    const mac = pick(/^Tolk-.*-macos\.dmg$/);
    if (!win || !mac) throw new Error("latest.json without installers");
    return { version: String(d.tag_name || "").replace(/^v/, ""), win, mac };
  } catch (e) {
    console.error(e);
    return FALLBACK;
  }
}

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const path = url.pathname;

    if (path === "/" || path === "/index.html") {
      return new Response(html, {
        headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "public, max-age=300" },
      });
    }
    if (path === "/icon.svg" || path === "/favicon.ico") {
      return new Response(icon, {
        headers: { "Content-Type": "image/svg+xml", "Cache-Control": "public, max-age=86400" },
      });
    }
    if (path === "/latest") {
      const d = await latest(ctx);
      return Response.json(
        { version: d.version, win: { name: d.win.name, size: d.win.size }, mac: { name: d.mac.name, size: d.mac.size } },
        { headers: { "Cache-Control": "public, max-age=300" } },
      );
    }
    const m = path.match(/^\/download\/(win|mac)\/?$/);
    if (m) {
      const d = await latest(ctx);
      const via = url.searchParams.get("via") || "github";
      const target = (MIRRORS[via] || "") + d[m[1]].url;
      return new Response(null, { status: 302, headers: { Location: target, "Cache-Control": "no-store" } });
    }
    return Response.redirect(url.origin + "/", 302);
  },
};
