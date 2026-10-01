package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 本文件的系统侧小工具：目录/权限/自安装/仓库识别。

func itoa(n int) string { return strconv.Itoa(n) }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func mustMkdirAll(dir string, mode os.FileMode) {
	if err := os.MkdirAll(dir, mode); err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] 建目录 %s：%v\n", dir, err)
		os.Exit(1)
	}
	_ = os.Chmod(dir, mode)
}

// write600 写文件并压成 600（无论如何都私密）。
func write600(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] 写 %s：%v\n", path, err)
		os.Exit(1)
	}
	_ = os.Chmod(path, 0o600)
}

// resolveRepoRoot：显式传入优先；否则 cwd 若同时有 AGENTS.md 与 hooks/ 目录，
// 视为仓库根。认不出来返回空串（调用方跳过仓库相关步骤）。
func resolveRepoRoot(flag string) string {
	if flag != "" {
		abs, err := filepath.Abs(flag)
		if err == nil {
			return abs
		}
		return flag
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if st, err := os.Stat(filepath.Join(cwd, "AGENTS.md")); err == nil && !st.IsDir() {
		if st, err := os.Stat(filepath.Join(cwd, "hooks")); err == nil && st.IsDir() {
			return cwd
		}
	}
	return ""
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// cleanStaleSockets 清理 ~/.ssh/cm 下没有进程持有的 socket（与原 bash 逻辑一致）。
func cleanStaleSockets(cmDir string) {
	entries, err := os.ReadDir(cmDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sock") {
			continue
		}
		p := filepath.Join(cmDir, e.Name())
		if st, err := os.Stat(p); err != nil || st.Mode()&os.ModeSocket == 0 {
			continue
		}
		if out, err := exec.Command("pgrep", "-f", "ControlPath="+p).Output(); err != nil || len(bytes.TrimSpace(out)) == 0 {
			_ = os.Remove(p)
		}
	}
}

// normalizeKeyPerms 权限归一：.pub → 644，其余 id_*/github_*/ *_deploy → 600；config/known_hosts → 600。
func normalizeKeyPerms(sshDir string) {
	for _, pat := range []string{"id_*", "*_deploy", "github_*"} {
		matches, _ := filepath.Glob(filepath.Join(sshDir, pat))
		for _, f := range matches {
			st, err := os.Stat(f)
			if err != nil || st.IsDir() {
				continue
			}
			if strings.HasSuffix(f, ".pub") {
				_ = os.Chmod(f, 0o644)
			} else {
				_ = os.Chmod(f, 0o600)
			}
		}
	}
	for _, name := range []string{"config", "known_hosts"} {
		if st, err := os.Stat(filepath.Join(sshDir, name)); err == nil && !st.IsDir() {
			_ = os.Chmod(filepath.Join(sshDir, name), 0o600)
		}
	}
}

// writeBashrcBlock 幂等写入 PATH/KEY/VL 快捷变量（守卫串：arena-config：快捷变量）。
func writeBashrcBlock() {
	rc := filepath.Join(os.Getenv("HOME"), ".bashrc")
	b, err := os.ReadFile(rc)
	if err != nil {
		return
	}
	if strings.Contains(string(b), "arena-config：快捷变量") {
		return
	}
	block := `
# arena-config：快捷变量（由 arena import 写入）
export PATH="$HOME/bin:$PATH"          # ~/bin 跨快照保留；/usr/local/bin 会丢
export KEY=~/.ssh/id_ed25519
export VL="ssh $(grep -oP "ARENA_TARGET_ALIAS=.\K[^\x27]+" ~/.arena-secrets/env.sh 2>/dev/null || echo target)"
`
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(block)
}

// selfInstall 把当前可执行文件装到 ~/bin/arena；已有不同版本先备份。
func selfInstall() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(os.Getenv("HOME"), "bin", "arena")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	cur, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, cur) {
		return nil // 已一致
	}
	if _, err := os.Stat(dst); err == nil {
		_ = os.Rename(dst, fmt.Sprintf("%s.bak.%d", dst, time.Now().Unix()))
	}
	if err := os.WriteFile(dst, cur, 0o755); err != nil {
		return err
	}
	return os.Chmod(dst, 0o755)
}

// copyFile 便捷复制（保留内容不保留元数据）。
func copyFile(dst, src string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}
