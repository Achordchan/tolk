// Tolk problem-report relay (Cloudflare Worker).
//
// The app POSTs a scrubbed report to /v1/report; this Worker files it as a
// GitHub issue with a token only it holds. Automatic reports of the same
// error (same fingerprint) are added to one issue as comments. KV keeps the
// rate limits and the fingerprint -> issue map.
//
// Bindings: KV namespace REPORTS; secret GITHUB_TOKEN (fine-grained, Issues:
// read and write on the repo only); var REPO ("Achordchan/tolk").

const MAX_PAYLOAD = 200 * 1024;
const MAX_BODY = 62000;
const LIMITS = {
  installPerDay: 12, // reports one install may file a day
  ipPerDay: 40, // reports one address may file a day
  manualPerInstallPerDay: 6,
  commentsPerIssuePerDay: 30, // repeats added to one issue a day
};
const KINDS = new Set(["manual", "error", "crash", "update"]);
const LABELS = { manual: ["用户反馈"], error: ["自动上报"], crash: ["自动上报", "崩溃"], update: ["自动上报", "更新"] };

const json = (data, status = 200) =>
  new Response(JSON.stringify(data), { status, headers: { "content-type": "application/json; charset=utf-8" } });

// Same idea as the app's scrubber, in case an old or modified client sends
// something it should not.
function scrub(s) {
  return String(s ?? "")
    .replace(/(bearer\s+)[A-Za-z0-9._\-]{8,}/gi, "$1***")
    .replace(/\bsk-[A-Za-z0-9_\-]{8,}/g, "sk-***")
    .replace(/\b(ghp|gho|ghu|ghs|github_pat)_[A-Za-z0-9_]{10,}/g, "$1_***")
    .replace(/\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(:fx)?\b/gi, "***-uuid-***")
    .replace(/((?:\b|_)(?:api[_-]?key|token|secret|password|authkey|key)["']?\s*[:=]\s*["']?)[^\s"'&,}]{6,}/gi, "$1***")
    .replace(/[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}/g, "***@***");
}

const clip = (s, n) => (s.length > n ? s.slice(0, n - 1) + "…" : s);
const today = () => new Date().toISOString().slice(0, 10);

async function bump(env, key, limit) {
  const n = parseInt((await env.REPORTS.get(key)) || "0", 10);
  if (n >= limit) return false;
  await env.REPORTS.put(key, String(n + 1), { expirationTtl: 2 * 86400 });
  return true;
}

async function gh(env, method, path, body) {
  const resp = await fetch(`https://api.github.com/repos/${env.REPO}${path}`, {
    method,
    headers: {
      authorization: `Bearer ${env.GITHUB_TOKEN}`,
      accept: "application/vnd.github+json",
      "x-github-api-version": "2022-11-28",
      "user-agent": "tolk-report-relay",
      ...(body ? { "content-type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(`GitHub ${resp.status}: ${data.message || "error"}`);
  return data;
}

async function handle(request, env) {
  if (!env.GITHUB_TOKEN || !env.REPORTS) return json({ ok: false, error: "relay not configured" }, 503);
  if (request.headers.get("x-tolk-report") !== "1") return json({ ok: false, error: "bad client" }, 400);
  const raw = await request.text();
  if (raw.length > MAX_PAYLOAD) return json({ ok: false, error: "report too large" }, 413);
  let r;
  try {
    r = JSON.parse(raw);
  } catch {
    return json({ ok: false, error: "bad json" }, 400);
  }
  const kind = String(r.kind || "");
  const install = String(r.installId || "").replace(/[^0-9a-f]/gi, "").slice(0, 32);
  const fp = String(r.fingerprint || "").replace(/[^0-9a-f]/gi, "").slice(0, 40);
  const version = String(r.version || "").replace(/[^0-9A-Za-z.\-+]/g, "").slice(0, 32);
  if (!KINDS.has(kind) || !install || !version || typeof r.body !== "string") {
    return json({ ok: false, error: "missing fields" }, 400);
  }
  if (kind !== "manual" && !fp) return json({ ok: false, error: "missing fingerprint" }, 400);

  const day = today();
  const ip = request.headers.get("cf-connecting-ip") || "unknown";
  if (!(await bump(env, `rl:ip:${ip}:${day}`, LIMITS.ipPerDay))) return json({ ok: false, skipped: "今天提交得太多了，请明天再试" }, 429);
  if (!(await bump(env, `rl:in:${install}:${day}`, LIMITS.installPerDay))) return json({ ok: false, skipped: "今天提交得太多了，请明天再试" }, 429);
  if (kind === "manual" && !(await bump(env, `rl:man:${install}:${day}`, LIMITS.manualPerInstallPerDay))) {
    return json({ ok: false, skipped: "今天提交得太多了，请明天再试" }, 429);
  }

  const os = String(r.os || "").replace(/[^a-z0-9]/gi, "").slice(0, 16);
  const title = clip(scrub(r.title || "问题反馈").replace(/\s+/g, " ").trim(), 140);
  const footer = `\n\n---\n<sub>由 Tolk v${version}（${os}）应用内提交 · relay</sub>`;
  const body = clip(scrub(r.body), MAX_BODY) + footer;

  // Repeats of an automatic report go on the issue already open for it.
  if (fp) {
    const known = await env.REPORTS.get(`fp:${fp}`);
    if (known) {
      const issue = await gh(env, "GET", `/issues/${known}`).catch(() => null);
      if (issue && issue.state === "open") {
        // One comment per install per day, and a cap per issue.
        if (await env.REPORTS.get(`seen:${fp}:${install}:${day}`)) {
          return json({ ok: true, number: issue.number, url: issue.html_url, merged: true });
        }
        if (!(await bump(env, `rl:cm:${fp}:${day}`, LIMITS.commentsPerIssuePerDay))) {
          return json({ ok: true, number: issue.number, url: issue.html_url, merged: true });
        }
        await env.REPORTS.put(`seen:${fp}:${install}:${day}`, "1", { expirationTtl: 2 * 86400 });
        await gh(env, "POST", `/issues/${issue.number}/comments`, { body: `又出现了一次（v${version}，${os}）\n\n` + body });
        return json({ ok: true, number: issue.number, url: issue.html_url, merged: true });
      }
    }
  }

  const labels = [...LABELS[kind], os === "windows" ? "Windows" : os === "darwin" ? "macOS" : os].filter(Boolean);
  // A token without the right to create labels gets a 422 for a new label;
  // the issue matters more than its labels.
  const issue = await gh(env, "POST", "/issues", { title, body, labels }).catch((e) =>
    String(e.message).includes("422") || String(e.message).includes("403") ? gh(env, "POST", "/issues", { title, body }) : Promise.reject(e),
  );
  if (fp) await env.REPORTS.put(`fp:${fp}`, String(issue.number));
  if (fp) await env.REPORTS.put(`seen:${fp}:${install}:${day}`, "1", { expirationTtl: 2 * 86400 });
  return json({ ok: true, number: issue.number, url: issue.html_url, merged: false });
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === "/" || url.pathname === "/health") return json({ ok: true, service: "tolk-report" });
    if (url.pathname !== "/v1/report") return json({ ok: false, error: "not found" }, 404);
    if (request.method !== "POST") return json({ ok: false, error: "method not allowed" }, 405);
    try {
      return await handle(request, env);
    } catch (e) {
      return json({ ok: false, error: String(e && e.message ? e.message : e) }, 502);
    }
  },
};
