# tolk-report

The relay behind Tolk's 提交问题 button and automatic error reports. The app
POSTs a scrubbed report to `/v1/report`; this Cloudflare Worker files it as an
issue in `Achordchan/tolk`. The GitHub token lives only here.

- Repeats of an automatic report (same fingerprint) become comments on the
  open issue for it: one per install per day, at most 30 a day per issue. A
  closed issue gets a fresh one if the error comes back.
- Limits per day: 12 reports per install, 6 of them manual, 40 per address.
- Reports are scrubbed again here (keys, tokens, e-mail addresses). The
  contact the user typed arrives as its own field and is added back after
  scrubbing, since they asked for it to be shown.
- All of it runs in one Durable Object (`Coordinator`) that takes reports one
  at a time, so limits and the fingerprint → issue map hold under concurrency.

## Setup

| Binding | Value |
| --- | --- |
| Durable Object `COORD` | class `Coordinator` (SQLite, migration tag `v1`) |
| Variable `REPO` | `Achordchan/tolk` |
| Secret `GITHUB_TOKEN` | fine-grained token: repository `tolk` only, Issues read and write |

Routes: the custom domain `report.achord.cn` (reachable from mainland China)
and `tolk-report.achordchan.workers.dev` as the spare. The app tries them in
that order (`internal/report/client.go`).

Redeploying through the API must keep the secret: upload with
`"keep_bindings": ["secret_text"]` in the metadata.
