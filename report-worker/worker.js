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
  // Looking for an interrupted attempt costs GitHub requests too.
  reconcilePerIpPerDay: 20,
  reconcilePerSubmissionPerDay: 5,
};
// Pages of 100 read when looking for an interrupted attempt's issue or
// comment; a Worker may make only so many requests per call.
const RECONCILE_PAGES = 20;
// A GitHub call that hangs would hold up every report behind it.
const GITHUB_TIMEOUT_MS = 15000;
// GitHub may still be filing a write whose request timed out: until this
// long after it, not finding it proves nothing, and writing again could
// file it twice.
const SETTLE_MS = 120e3;
const STILL_PENDING = { ok: false, pending: true, error: "上一次提交还在处理中，请过一两分钟再重试" };
const KINDS = new Set(["manual", "error", "crash", "update"]);
const LABELS = { manual: ["用户反馈"], error: ["自动上报"], crash: ["自动上报", "崩溃"], update: ["自动上报", "更新"] };
const TOO_MANY = "今天提交得太多了，请明天再试";

const json = (data, status = 200) =>
  new Response(JSON.stringify(data), { status, headers: { "content-type": "application/json; charset=utf-8" } });

// A setting whose value is a credential, up to the value. The name may be
// quoted, also JSON-escaped (\"password\").
const SECRET_KEY = String.raw`((?:\b|_)(?:api[_-]?key|access[_-]?key|secret[_-]?key|auth[_-]?key|token|secret|password|passwd|key)(?:\\?["'])?\s*[:=]`;
// Parts are bounded (RFC 5321 sizes): unbounded ones cost quadratic time on
// a long run of letters with no @ in it.
const EMAIL = /[A-Za-z0-9._%+\-]{1,64}@[A-Za-z0-9\-]{1,63}(?:\.[A-Za-z0-9\-]{1,63}){0,8}\.[A-Za-z]{2,24}/g;
// A credential setting up to its = or :.
const SECRET_ASSIGN = new RegExp(SECRET_KEY + ")", "gi");

// Skips what may stand between a setting's = or : and its value: spaces and
// line breaks, also JSON-escaped ones (\n, \r, \t) in a log that holds
// pretty-printed JSON.
function valueStart(s, i) {
  while (i < s.length) {
    if (" \t\r\n".includes(s[i])) i++;
    else if (s[i] === "\\" && i + 1 < s.length && "nrt".includes(s[i + 1])) i += 2;
    else break;
  }
  return i;
}

// The quote a value at i opens with: ", ', \" or \' (JSON in a JSON string),
// or "" for an unquoted value.
function opener(s, i) {
  if (s[i] === '"' || s[i] === "'") return s[i];
  if (s[i] === "\\" && (s[i + 1] === '"' || s[i + 1] === "'")) return s.slice(i, i + 2);
  return "";
}

// Blanks the whole value of every credential setting. A quoted value goes
// up to its closing quote, escaped quotes and backslashes included, or for a
// value cut off (a truncated log line) to the end of the line; an unquoted
// one up to the next whitespace, whatever characters it holds. A scan rather
// than a pattern: one pass, nothing to retry, no way to stop early at an
// escape or a symbol. Same as scrubSecrets in internal/report/report.go.
function scrubSecrets(s) {
  let out = "";
  let pos = 0;
  SECRET_ASSIGN.lastIndex = 0;
  for (let m; (m = SECRET_ASSIGN.exec(s)); ) {
    let start = valueStart(s, m.index + m[0].length);
    const open = opener(s, start);
    let end = start;
    if (open) {
      start += open.length;
      end = quotedEnd(s, start, open);
    } else while (end < s.length && !" \t\r\n".includes(s[end])) end++;
    out += s.slice(pos, start) + (end > start ? "***" : "");
    pos = end;
    SECRET_ASSIGN.lastIndex = Math.max(end, start);
  }
  return out + s.slice(pos);
}

const nl = (c) => c === "\r" || c === "\n";

// Where the value starting at i, opened by open, ends: its closing quote,
// the end of the line, or the end of s.
function quotedEnd(s, i, open) {
  const q = open[open.length - 1];
  if (open.length === 1) {
    // "…" or '…': \x is an escape
    while (i < s.length) {
      const c = s[i];
      if (c === "\\") {
        if (i + 1 >= s.length || nl(s[i + 1])) return i + 1;
        i += 2;
      } else if (c === q || nl(c)) return i;
      else i++;
    }
    return s.length;
  }
  // \"…\" inside a JSON string. Escaped once: \" ends the value, a raw "
  // ends the outer string, \x is one character. \\ is a backslash of the
  // inner text, which escapes the character after it (\\\" is a quote).
  while (i < s.length) {
    const c = s[i];
    if (c === "\\" && s[i + 1] === q) return i;
    if (c === "\\" && s[i + 1] === "\\") {
      i += 2;
      if (s[i] === "\\" && i + 1 < s.length && !nl(s[i + 1])) i += 2;
      else if (i < s.length && s[i] !== '"' && s[i] !== "\\" && !nl(s[i])) i++;
    } else if (c === "\\") {
      if (i + 1 >= s.length || nl(s[i + 1])) return i + 1;
      i += 2;
    } else if (c === '"' || nl(c)) return i;
    else i++;
  }
  return s.length;
}

// Same idea as the app's scrubber, in case an old or modified client sends
// something it should not. The contact the user typed is added afterwards:
// they asked for it to be shown.
export function scrub(s) {
  return scrubSecrets(String(s ?? ""))
    .replace(/(bearer\s+)[A-Za-z0-9._\-]{8,}/gi, "$1***")
    .replace(/\bsk-[A-Za-z0-9_\-]{8,}/g, "sk-***")
    .replace(/\b(ghp|gho|ghu|ghs|github_pat)_[A-Za-z0-9_]{10,}/g, "$1_***")
    .replace(/\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(:fx)?\b/gi, "***-uuid-***")
    .replace(EMAIL, "***@***");
}

// Hidden in every issue and comment the relay writes, so a retry can tell
// whether an interrupted attempt already got through.
const marker = (sub) => `<!-- tolk-submission:${sub} -->`;

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
      const stale = [];
      for (const prefix of ["rl:", "seen:"]) stale.push(...(await this.storage.list({ prefix })).keys());
      // Submission records outlive the day: a retry just after midnight
      // must still find the one filed just before. They go after a week.
      const week = Date.now() - 7 * 86400e3;
      for (const prefix of ["sub:", "pend:", "pfp:"]) {
        for (const [k, v] of await this.storage.list({ prefix })) if (!(v && v.at > week)) stale.push(k);
      }
      // delete() takes at most 128 keys a call.
      for (let i = 0; i < stale.length; i += 128) await this.storage.delete(stale.slice(i, i + 128));
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
      // A write that times out may still have gone through: its pend: record
      // stays (refused() keeps it for anything but a 4xx), and the retry checks.
      signal: AbortSignal.timeout(GITHUB_TIMEOUT_MS),
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
      if (done) return [200, done.data];
      // An earlier attempt reached GitHub but never saw the answer (the
      // connection dropped): look for what it wrote before writing again.
      const pend = await this.storage.get(`pend:${sub}`);
      if (pend) {
        // Searching GitHub is paid for like filing: a client resending one
        // submission cannot use the relay's GitHub quota without limit.
        if (!(await this.bump(`rc:${ip}`, LIMITS.reconcilePerIpPerDay))) return [429, { ok: false, skipped: TOO_MANY }];
        if (!(await this.bump(`rcs:${sub}`, LIMITS.reconcilePerSubmissionPerDay))) return [429, { ok: false, skipped: TOO_MANY }];
        const found = await this.reconcile(sub, pend, fp, install);
        if (found) {
          await this.storage.put(`sub:${sub}`, { at: Date.now(), data: found });
          await this.storage.delete(`pend:${sub}`);
          return [200, found];
        }
        if (Date.now() - pend.at < SETTLE_MS) return [409, STILL_PENDING];
      }
    }
    const [status, data] = await this.file(ip, r, kind, install, fp, version, sub);
    if (sub && status === 200) {
      await this.storage.put(`sub:${sub}`, { at: Date.now(), data });
      await this.storage.delete(`pend:${sub}`);
    }
    return [status, data];
  }

  // Finds the issue or comment an interrupted attempt created, by the
  // marker every write carries. Every page written since the attempt is
  // read: "not found" has to mean it did not get through, or the retry
  // files a duplicate.
  async reconcile(sub, pend, fp, install) {
    const mark = marker(sub);
    const cutoff = pend.at - 60e3;
    if (pend.issue) {
      const since = new Date(cutoff).toISOString();
      const hit = await this.scan(`/issues/${pend.issue}/comments?since=${since}`, () => false, (c) =>
        String(c.body || "").includes(mark),
      );
      if (!hit) return null;
      await this.storage.put(`seen:${fp}:${install}`, 1);
      return { ok: true, number: pend.issue, url: pend.url, merged: true };
    }
    const hit = await this.findIssue(sub, pend.at);
    if (!hit) return null;
    if (fp) {
      await this.storage.put(`fp:${fp}`, hit.number);
      await this.storage.put(`seen:${fp}:${install}`, 1);
      await this.storage.delete(`pfp:${fp}`);
    }
    return { ok: true, number: hit.number, url: hit.html_url, merged: false };
  }

  // The issue a creation attempt for sub, started at at, opened, if any.
  // Newest first, so the pages end once they reach issues older than that.
  async findIssue(sub, at) {
    const mark = marker(sub);
    return this.scan(
      "/issues?state=all&sort=created&direction=desc",
      (i) => Date.parse(i.created_at) < at - 60e3,
      (i) => !i.pull_request && String(i.body || "").includes(mark),
    );
  }

  // Pages through a GitHub list until match finds an item, past says the
  // rest is too old, or the list ends. Too many pages to read is an error,
  // not "not found": the client retries later instead of filing twice.
  async scan(path, past, match) {
    for (let page = 1; page <= RECONCILE_PAGES; page++) {
      const items = await this.gh("GET", `${path}&per_page=100&page=${page}`);
      for (const it of items) {
        if (match(it)) return it;
        if (past(it)) return null;
      }
      if (items.length < 100) return null;
    }
    throw new Error("too many items to check for an earlier attempt");
  }

  // GitHub answered a write with a 4xx: nothing was written, so a retry has
  // nothing to look for. Only a lost answer (network, 5xx) keeps pend:.
  async refused(sub, e, fp) {
    if (sub && /GitHub 4\d\d/.test(String(e && e.message))) {
      await this.storage.delete(`pend:${sub}`);
      if (fp) await this.storage.delete(`pfp:${fp}`);
    }
    throw e;
  }

  async file(ip, r, kind, install, fp, version, sub) {
    if (!(await this.bump(`ip:${ip}`, LIMITS.ipPerDay))) return [429, { ok: false, skipped: TOO_MANY }];
    if (!(await this.bump(`in:${install}`, LIMITS.installPerDay))) return [429, { ok: false, skipped: TOO_MANY }];
    if (kind === "manual" && !(await this.bump(`man:${install}`, LIMITS.manualPerInstallPerDay))) {
      return [429, { ok: false, skipped: TOO_MANY }];
    }

    const os = String(r.os || "").replace(/[^a-z0-9]/gi, "").slice(0, 16);
    const title = clip(scrub(r.title || "").replace(/\s+/g, " ").trim(), 140) || "问题反馈";
    const body = issueBody(r, version, os) + (sub ? "\n" + marker(sub) : "");

    // Repeats of an automatic report go on the issue already open for it.
    if (fp) {
      let known = await this.storage.get(`fp:${fp}`);
      // A creation for this error whose answer was lost (a new issue, or the
      // replacement of a closed one): whoever reports the error next (any
      // install) first checks whether it went through, so the error does not
      // get a second issue.
      const pending = await this.storage.get(`pfp:${fp}`);
      if (pending) {
        const hit = await this.findIssue(pending.sub, pending.at);
        if (hit) {
          known = hit.number;
          await this.storage.put(`fp:${fp}`, known);
        } else if (Date.now() - pending.at < SETTLE_MS) {
          return [409, STILL_PENDING];
        }
        await this.storage.delete(`pfp:${fp}`);
      }
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
          if (sub) await this.storage.put(`pend:${sub}`, { at: Date.now(), issue: issue.number, url: issue.html_url });
          await this.gh("POST", `/issues/${issue.number}/comments`, { body: `又出现了一次（v${version}，${os}）\n\n` + body }).catch((e) =>
            this.refused(sub, e),
          );
          await this.storage.put(`seen:${fp}:${install}`, 1);
          return merged;
        }
      }
    }

    if (sub) await this.storage.put(`pend:${sub}`, { at: Date.now() });
    if (sub && fp) await this.storage.put(`pfp:${fp}`, { at: Date.now(), sub });
    const labels = [...LABELS[kind], os === "windows" ? "Windows" : os === "darwin" ? "macOS" : os].filter(Boolean);
    // A token without the right to create labels gets a 422 for a new label;
    // the issue matters more than its labels.
    const issue = await this.gh("POST", "/issues", { title, body, labels })
      .catch((e) => (/GitHub (403|422)/.test(String(e.message)) ? this.gh("POST", "/issues", { title, body }) : Promise.reject(e)))
      .catch((e) => this.refused(sub, e, fp));
    if (fp) {
      await this.storage.put(`fp:${fp}`, issue.number);
      await this.storage.put(`seen:${fp}:${install}`, 1);
      await this.storage.delete(`pfp:${fp}`);
    }
    return [200, { ok: true, number: issue.number, url: issue.html_url, merged: false }];
  }
}

// The body as text, or null once it passes max bytes (reading stops there).
async function readCapped(request, max) {
  if (!request.body) return "";
  const reader = request.body.getReader();
  const chunks = [];
  let size = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > max) {
      await reader.cancel();
      return null;
    }
    chunks.push(value);
  }
  const all = new Uint8Array(size);
  let at = 0;
  for (const c of chunks) {
    all.set(c, at);
    at += c.byteLength;
  }
  return new TextDecoder().decode(all);
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === "/" || url.pathname === "/health") return json({ ok: true, service: "tolk-report" });
    if (url.pathname !== "/v1/report") return json({ ok: false, error: "not found" }, 404);
    if (request.method !== "POST") return json({ ok: false, error: "method not allowed" }, 405);
    if (!env.GITHUB_TOKEN || !env.COORD) return json({ ok: false, error: "relay not configured" }, 503);
    if (request.headers.get("x-tolk-report") !== "1") return json({ ok: false, error: "bad client" }, 400);
    // Refuse an oversized body before holding it in memory: by its declared
    // length, and while reading for a body that does not declare one.
    if (Number(request.headers.get("content-length") || 0) > MAX_PAYLOAD) {
      return json({ ok: false, error: "report too large" }, 413);
    }
    const raw = await readCapped(request, MAX_PAYLOAD);
    if (raw === null) return json({ ok: false, error: "report too large" }, 413);
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
