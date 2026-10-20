package update

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxPackageBytes    = 400 << 20
	maxZipMembers      = 20000
	maxZipUncompressed = 800 << 20
	maxZipRatio        = 100.0
)

// Download fetches the package into dir and returns its path. It tries the
// release's own URL, then the same asset on the other sources. The result is
// not trusted until Verify has passed.
func (c *Checker) Download(ctx context.Context, info *Info, dir string, progress func(done, total int64)) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	urls := append([]string{info.url}, c.mirrorURLs(ctx, info)...)
	var errs []string
	for _, u := range urls {
		path, err := c.fetchTo(ctx, u, filepath.Join(dir, filepath.Base(info.name)), info.wantSize, progress)
		if err == nil {
			return path, nil
		}
		errs = append(errs, err.Error())
	}
	return "", fmt.Errorf("下载更新包失败：%s", strings.Join(errs, "；"))
}

func (c *Checker) fetchTo(ctx context.Context, url, dest string, want int64, progress func(done, total int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	client := *c.Client
	client.Timeout = 0 // the context bounds the download, not a fixed clock
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	if total > maxPackageBytes || want > maxPackageBytes {
		return "", errors.New("更新包体积过大")
	}
	tmp := dest + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxPackageBytes {
				f.Close()
				os.Remove(tmp)
				return "", errors.New("更新包体积过大")
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return "", werr
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return "", rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if want > 0 && done != want {
		os.Remove(tmp)
		return "", errors.New("更新包大小与发布信息不一致")
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

// Verify checks the downloaded package against the embedded public keys.
func Verify(info *Info, path string) error {
	sum, size, err := SHA256File(path)
	if err != nil {
		return err
	}
	if info.wantSize > 0 && size != info.wantSize {
		return errors.New("更新包大小与发布信息不一致，已终止更新")
	}
	if info.sig == "" {
		return errors.New("更新包缺少有效签名，已终止更新")
	}
	switch info.pkgGOOS {
	case "windows":
		if info.sha != "" && sum != info.sha {
			return errors.New("更新包校验值与发布信息不一致，已终止更新")
		}
		return VerifyPayload(WindowsPublicKeyB64, Payload{
			AppVersion: info.Version, Filename: info.name, PackageType: "windows_full_update",
			Platform: "windows", SHA256: sum, SizeBytes: size,
		}, info.sig)
	case "darwin":
		return VerifyFileBytes(SparklePublicKeyB64, path, info.sig)
	}
	return errors.New("这个系统暂不支持自动更新")
}

// ExtractZip unpacks a zip into dest, refusing path escapes, zip bombs and
// anything but plain files and directories.
func ExtractZip(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return errors.New("更新包不是有效的 zip 文件")
	}
	defer zr.Close()
	if len(zr.File) > maxZipMembers {
		return fmt.Errorf("更新包文件数过多：%d", len(zr.File))
	}
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		total += f.UncompressedSize64
		if total > maxZipUncompressed {
			return errors.New("更新包解压后体积过大")
		}
		if f.CompressedSize64 > 0 && f.UncompressedSize64 > 1<<20 &&
			float64(f.UncompressedSize64)/float64(f.CompressedSize64) > maxZipRatio {
			return errors.New("更新包压缩比异常")
		}
	}
	for _, f := range zr.File {
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return errors.New("更新包包含非法路径")
		}
		mode := f.FileInfo().Mode()
		switch {
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			return errors.New("更新包包含符号链接")
		case mode.IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeZipFile(f, target, mode.Perm()|0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeZipFile(f *zip.File, target string, perm os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
