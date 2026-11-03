// Tolk problem-report relay (Cloudflare Worker).
//
// The app POSTs a scrubbed report to /v1/report; this Worker files it as a
// GitHub issue with a token only it holds. Automatic reports of the same
// error (same fingerprint) are added to one issue as comments.
//
// Every report goes through one Durable Object (Coordinator) that handles
// them one at a time, so the rate limits and the fingerprint -> issue map are
// exact: two reports arriving together cannot both pass a limit or both open
// an issue for the same new error. Report volume is tiny, so one instance is
// plenty.
//
// Bindings: Durable Object COORD (class Coordinator); secret GITHUB_TOKEN
// (fine-grained, Issues: read and write on the repo only); var REPO.

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
const TOO_MANY = "今天提交得太多了，请明天再试";

const json = (data, status = 200) =>
  new Response(JSON.stringify(data), { status, headers: { "content-type": "application/json; charset=utf-8" } });

// Same idea as the app's scrubber, in case an old or modified client sends
// something it should not. The contact the user typed is added afterwards:
// they asked for it to be shown.
export function scrub(s) {
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

// The issue body: scrubbed report, the contact under the description, a footer.
export function issueBody(r, version, os) {
  let body = clip(scrub(r.body), MAX_BODY);
  // Shown exactly as typed: Markdown characters are escaped, never dropped
  // (an address like first_last@example.com must stay intact).
  const contact = String(r.contact || "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 100)
    .replace(/[\\`*_[\]|~#!()]/g, "\\$&")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
  if (contact) {
    const line = `**联系方式：** ${contact}\n\n`;
    const at = body.indexOf("### 环境");
    body = at >= 0 ? body.slice(0, at) + line + body.slice(at) : line + body;
  }
  return body + `\n\n---\n<sub>由 Tolk v${version}（${os}）应用内提交 · relay</sub>`;
}

export class Coordinator {
  constructor(state, env) {
    this.storage = state.storage;
    this.env = env;
    this.queue = Promise.resolve();
  }

  async fetch(request) {
    const { ip, report } = await request.json();
    // One report at a time, start to finish, GitHub calls included.
    const run = this.queue.then(() => this.handle(ip, report));
    this.queue = run.catch(() => {});
    try {
      const [status, data] = await run;
      return json(data, status);
    } catch (e) {
      return json({ ok: false, error: String(e && e.message ? e.message : e) }, 502);
    }
  }

  // Counters are per day; the first report of a new day drops yesterday's.
  async bump(key, limit) {
    const day = today();
    if ((await this.storage.get("day")) !== day) {
      for (const prefix of ["rl:", "seen:", "sub:"]) {
        const keys = [...(await this.storage.list({ prefix })).keys()];
        // delete() takes at most 128 keys a call.
        for (let i = 0; i < keys.length; i += 128) await this.storage.delete(keys.slice(i, i + 128));
      }
      await this.storage.put("day", day);
    }
    const n = (await this.storage.get("rl:" + key)) || 0;
    if (n >= limit) return false;
    await this.storage.put("rl:" + key, n + 1);
    return true;
  }

  async gh(method, path, body) {
    const resp = await fetch(`https://api.github.com/repos/${this.env.REPO}${path}`, {
      method,
      headers: {
        authorization: `Bearer ${this.env.GITHUB_TOKEN}`,
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

  async handle(ip, r) {
    const kind = String(r.kind || "");
    const install = String(r.installId || "").replace(/[^0-9a-f]/gi, "").slice(0, 32);
    const fp = String(r.fingerprint || "").replace(/[^0-9a-f]/gi, "").slice(0, 40);
    const version = String(r.version || "").replace(/[^0-9A-Za-z.\-+]/g, "").slice(0, 32);
    if (!KINDS.has(kind) || !install || !version || typeof r.body !== "string") {
      return [400, { ok: false, error: "missing fields" }];
    }
    if (kind !== "manual" && !fp) return [400, { ok: false, error: "missing fingerprint" }];

    // A repeat of a submission already handled gets the same answer.
    const sub = String(r.submissionId || "").replace(/[^0-9a-f]/gi, "").slice(0, 40);
    if (sub) {
      const done = await this.storage.get(`sub:${sub}`);
      if (done) return [200, done];
    }
    const [status, data] = await this.file(ip, r, kind, install, fp, version);
    if (sub && status === 200) await this.storage.put(`sub:${sub}`, data);
    return [status, data];
  }

  async file(ip, r, kind, install, fp, version) {
    if (!(await this.bump(`ip:${ip}`, LIMITS.ipPerDay))) return [429, { ok: false, skipped: TOO_MANY }];
    if (!(await this.bump(`in:${install}`, LIMITS.installPerDay))) return [429, { ok: false, skipped: TOO_MANY }];
    if (kind === "manual" && !(await this.bump(`man:${install}`, LIMITS.manualPerInstallPerDay))) {
      return [429, { ok: false, skipped: TOO_MANY }];
    }

    const os = String(r.os || "").replace(/[^a-z0-9]/gi, "").slice(0, 16);
    const title = clip(scrub(r.title || "问题反馈").replace(/\s+/g, " ").trim(), 140);
    const body = issueBody(r, version, os);

    // Repeats of an automatic report go on the issue already open for it.
    if (fp) {
      const known = await this.storage.get(`fp:${fp}`);
      if (known) {
        // Only a missing issue (deleted or transferred) counts as gone; any
        // other failure is passed on, or a GitHub hiccup would open a duplicate.
        const issue = await this.gh("GET", `/issues/${known}`).catch((e) =>
          /GitHub (404|410)/.test(String(e.message)) ? null : Promise.reject(e),
        );
        if (issue && issue.state === "open") {
          const merged = [200, { ok: true, number: issue.number, url: issue.html_url, merged: true }];
          // One comment per install per day, and a cap per issue.
          if (await this.storage.get(`seen:${fp}:${install}`)) return merged;
          if (!(await this.bump(`cm:${fp}`, LIMITS.commentsPerIssuePerDay))) return merged;
          await this.gh("POST", `/issues/${issue.number}/comments`, { body: `又出现了一次（v${version}，${os}）\n\n` + body });
          await this.storage.put(`seen:${fp}:${install}`, 1);
          return merged;
        }
      }
    }

    const labels = [...LABELS[kind], os === "windows" ? "Windows" : os === "darwin" ? "macOS" : os].filter(Boolean);
    // A token without the right to create labels gets a 422 for a new label;
    // the issue matters more than its labels.
    const issue = await this.gh("POST", "/issues", { title, body, labels }).catch((e) =>
      /GitHub (403|422)/.test(String(e.message)) ? this.gh("POST", "/issues", { title, body }) : Promise.reject(e),
    );
    if (fp) {
      await this.storage.put(`fp:${fp}`, issue.number);
      await this.storage.put(`seen:${fp}:${install}`, 1);
    }
    return [200, { ok: true, number: issue.number, url: issue.html_url, merged: false }];
  }
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === "/" || url.pathname === "/health") return json({ ok: true, service: "tolk-report" });
    if (url.pathname !== "/v1/report") return json({ ok: false, error: "not found" }, 404);
    if (request.method !== "POST") return json({ ok: false, error: "method not allowed" }, 405);
    if (!env.GITHUB_TOKEN || !env.COORD) return json({ ok: false, error: "relay not configured" }, 503);
    if (request.headers.get("x-tolk-report") !== "1") return json({ ok: false, error: "bad client" }, 400);
    const raw = await request.text();
    if (raw.length > MAX_PAYLOAD) return json({ ok: false, error: "report too large" }, 413);
    let report;
    try {
      report = JSON.parse(raw);
    } catch {
      return json({ ok: false, error: "bad json" }, 400);
    }
    const ip = request.headers.get("cf-connecting-ip") || "unknown";
    const stub = env.COORD.get(env.COORD.idFromName("global"));
    return stub.fetch("https://coordinator/report", { method: "POST", body: JSON.stringify({ ip, report }) });
  },
};
