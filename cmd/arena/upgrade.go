package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releaseRepo = "QuietSugar/arena-config"

// githubToken 返回 GITHUB_TOKEN / GH_TOKEN（私有仓库访问 Release 用）。
func githubToken() string {
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	return os.Getenv("GH_TOKEN")
}

// latestReleaseTag 用 /releases/latest 的重定向解析最新 tag（不走 API，不占配额）。
func latestReleaseTag(repo string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest("GET", "https://github.com/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("响应里没有 Location（私有仓库需要 GITHUB_TOKEN？）")
	}
	return loc[strings.LastIndex(loc, "/")+1:], nil
}

// selfUpdate 检查并安装 GitHub Release 上的更新版本（带 sha256 校验，旧版备份）。
// 任何网络/校验问题都返回 error —— 调用方降级为 WARN，不影响自检主流程。
func selfUpdate() error {
	if version == "dev" {
		return fmt.Errorf("开发构建（version=dev），跳过")
	}
	tag, err := latestReleaseTag(releaseRepo)
	if err != nil {
		return err
	}
	if tag == "v"+version {
		return nil
	}
	ver := strings.TrimPrefix(tag, "v")
	archive := fmt.Sprintf("arena_%s_%s_%s.tar.gz", ver, runtime.GOOS, runtime.GOARCH)
	base := "https://github.com/" + releaseRepo + "/releases/download/" + tag

	tmp, err := os.MkdirTemp("", "arena-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	dl := func(name string) ([]byte, error) {
		client := &http.Client{Timeout: 120 * time.Second}
		req, err := http.NewRequest("GET", base+"/"+name, nil)
		if err != nil {
			return nil, err
		}
		if tok := githubToken(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("下载 %s 返回 %s（私有仓库需要 GITHUB_TOKEN？）", name, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	archData, err := dl(archive)
	if err != nil {
		return err
	}
	sumsData, err := dl("checksums.txt")
	if err != nil {
		return err
	}
	// 期望行先取出来再校验（不要管道空过）
	var expected string
	for _, l := range strings.Split(string(sumsData), "\n") {
		if strings.HasSuffix(l, "  "+archive) || strings.HasSuffix(l, " "+archive) {
			expected = strings.Fields(l)[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksums.txt 里没有 %s —— 中止", archive)
	}
	sum := sha256.Sum256(archData)
	if got := hex.EncodeToString(sum[:]); got != expected {
		return fmt.Errorf("校验和不符（ got %s, want %s）—— 中止", got, expected)
	}

	gz, err := gzip.NewReader(bytes.NewReader(archData))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var binary []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == "arena" {
			binary, err = io.ReadAll(tr)
			if err != nil {
				return err
			}
			break
		}
	}
	if binary == nil {
		return fmt.Errorf("压缩包里找不到 arena 二进制")
	}
	dst := filepath.Join(os.Getenv("HOME"), "bin", "arena")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		_ = os.Rename(dst, fmt.Sprintf("%s.bak.%d", dst, time.Now().Unix()))
	}
	if err := os.WriteFile(dst, binary, 0o755); err != nil {
		return err
	}
	fmt.Printf("  自更新完成：%s → %s\n", version, tag)
	return nil
}

// tryInstallRsync 尽力用 apt 装上 rsync（与原 bash 行为一致；失败由调用方报 FAIL）。
func tryInstallRsync() error {
	if out, err := exec.Command("sudo", "apt-get", "update", "-qq").CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get update 失败：%v（%s）", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("sudo", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y", "-qq", "rsync").CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get install rsync 失败：%v（%s）", err, strings.TrimSpace(string(out)))
	}
	return nil
}
