# 官网 tolk.achord.cn

单页官网，跑在 Cloudflare Worker `tolk-site` 上，域名通过 Workers Custom Domain 绑定。

| 文件 | 内容 |
|---|---|
| `index.html` | 页面本身（样式、脚本都内联） |
| `icon.svg` | 图标，和 `design/icon.svg` 一样 |
| `worker.js` | 返回页面和图标；`/latest` 给出最新版本号和安装包大小；`/download/{win\|mac}?via=github\|mirror\|mirror2` 跳到最新安装包 |

下载链接不写死版本：Worker 读 GitHub 最新 release 里的 `latest.json`，挑出 Windows 安装器和 macOS dmg，缓存 10 分钟。发新版本不用改官网。镜像用的是 `ghfast.top` 和 `gh-proxy.com`，和 `internal/update/release.go` 的 `DefaultMirrors` 一致。GitHub 读不到时退回 `worker.js` 里写的 2.2.3。

## 本地预览

```sh
python3 -m http.server 8765 --directory site
```

`/latest` 和 `/download/...` 只有 Worker 上才有，本地预览时页面用内置的 2.2.3 数据，下载按钮点不通。

## 部署

没有 wrangler 配置。用 Cloudflare API 上传脚本（multipart）：

- `PUT /accounts/{account_id}/workers/scripts/tolk-site`
- `metadata`：`{"main_module": "worker.js", "compatibility_date": "2026-10-01"}`
- `worker.js` 类型 `application/javascript+module`；`index.html`、`icon.svg` 类型 `text/plain`（作为文本模块被 import）

部署后对比线上和本地：

```sh
curl -s https://tolk.achord.cn/ | diff site/index.html -
curl -s https://tolk.achord.cn/latest
curl -sI "https://tolk.achord.cn/download/win?via=mirror"
```
