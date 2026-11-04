package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DownloadArtifact reuses release routing for independently versioned plugins.
// The expected digest must come from the publisher, never the download mirror.
func (c *Checker) DownloadArtifact(ctx context.Context, url, digest, dir string, progress func(string, string, int64, int64)) (string, error) {
	raw := strings.TrimPrefix(digest, "sha256:")
	expected, err := hex.DecodeString(raw)
	if !strings.HasPrefix(digest, "sha256:") || err != nil || len(expected) != sha256.Size {
		return "", fmt.Errorf("插件校验信息无效")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if progress == nil {
		progress = func(string, string, int64, int64) {}
	}
	progress("测速", "", 0, 0)
	var failures []string
	for _, route := range c.rank(ctx, c.routes(url)) {
		c.logf("插件下载线路：%s", routeName(route, c.Origin))
		progress("连接下载线路", routeName(route, c.Origin), 0, 0)
		path, e := c.fetchTo(ctx, route, filepath.Join(dir, "plugin-download"), 0, func(done, total int64) { progress("下载", routeName(route, c.Origin), done, total) })
		if e == nil {
			progress("校验", routeName(route, c.Origin), 0, 0)
			f, openErr := os.Open(path)
			e = openErr
			if e == nil {
				h := sha256.New()
				_, e = io.Copy(h, f)
				f.Close()
				if e == nil && hex.EncodeToString(h.Sum(nil)) != raw {
					e = fmt.Errorf("插件 SHA-256 校验失败")
				}
			}
		}
		if e == nil {
			return path, nil
		}
		if path != "" {
			_ = os.Remove(path)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		failures = append(failures, fmt.Sprintf("%s：%v", routeName(route, c.Origin), e))
	}
	return "", fmt.Errorf("插件下载失败：%s", strings.Join(failures, "；"))
}
