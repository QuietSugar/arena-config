package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bootstrapHelpOutput 只走帮助分支，不执行环境修复、自更新或 SSH。
// runBootstrap 写进程 stdout；这些测试不能并行修改它。
func bootstrapHelpOutput(t *testing.T, args ...string) (string, int) {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "bootstrap-help-")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = output
	defer func() {
		os.Stdout = previous
		output.Close()
	}()

	code := runBootstrap(args)
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	return string(content), code
}

func recoveryCommandFromHelp(t *testing.T) string {
	t.Helper()
	help, code := bootstrapHelpOutput(t, "--help")
	if code != 0 {
		t.Fatalf("bootstrap --help exit = %d，want 0", code)
	}
	for _, line := range strings.Split(help, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "chmod ") {
			return line
		}
	}
	t.Fatalf("bootstrap 帮助缺少不依赖 arena 已可执行的 shell 急救命令：\n%s", help)
	return ""
}

func TestBootstrapHelpRecovery(t *testing.T) {
	for _, args := range [][]string{
		{"-h"},
		{"--help"},
		{"--json", "--help"},
		{"--no-self-update", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			help, code := bootstrapHelpOutput(t, args...)
			if code != 0 {
				t.Fatalf("help exit = %d，want 0", code)
			}
			for _, want := range []string{
				"arena bootstrap [--json] [--no-self-update]",
				"Permission denied",
				"执行位",
				`chmod 755 "$HOME/bin/arena" && "$HOME/bin/arena" bootstrap`,
				"自定义安装目录",
			} {
				if !strings.Contains(help, want) {
					t.Errorf("help 应包含 %q，实际：\n%s", want, help)
				}
			}
		})
	}
}

func TestBootstrapRecoveryCommand(t *testing.T) {
	command := recoveryCommandFromHelp(t)
	for _, tc := range []struct {
		name string
		mode os.FileMode
		exit int
	}{
		{"snapshot_0644", 0o644, 0},
		{"snapshot_0600", 0o600, 0},
		{"already_executable", 0o755, 0},
		{"bootstrap_failure_propagates", 0o644, 23},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 带空格和单引号的 HOME，且 arena 所在目录不加入 PATH。
			home := filepath.Join(t.TempDir(), "home with spaces 'quoted'")
			bin := filepath.Join(home, "bin")
			if err := os.MkdirAll(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			arena := filepath.Join(bin, "arena")
			// 假 arena 只记录参数与退出码；测试不接触真实配置、密钥或网络。
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > \"$HOME/bootstrap.args\"\nexit %d\n", tc.exit)
			if err := os.WriteFile(arena, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(arena, tc.mode); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(bin, "unrelated")
			if err := os.WriteFile(unrelated, []byte("leave this file unchanged\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.mode&0o111 == 0 {
				err := exec.Command(arena, "bootstrap").Run()
				if !errors.Is(err, os.ErrPermission) {
					t.Fatalf("执行位缺失时应无法启动 arena，实际：%v", err)
				}
			}

			cmd := exec.Command("sh", "-c", command)
			cmd.Env = append(os.Environ(), "HOME="+home)
			output, err := cmd.CombinedOutput()
			if tc.exit == 0 && err != nil {
				t.Fatalf("急救命令失败：%v\n%s", err, output)
			}
			if tc.exit != 0 {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != tc.exit {
					t.Fatalf("应保留 bootstrap 的退出码 %d，实际：%v\n%s", tc.exit, err, output)
				}
			}
			info, err := os.Stat(arena)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o755 {
				t.Errorf("恢复后权限 = %o，want 755", info.Mode().Perm())
			}
			args, err := os.ReadFile(filepath.Join(home, "bootstrap.args"))
			if err != nil {
				t.Fatal(err)
			}
			if string(args) != "bootstrap\n" {
				t.Errorf("恢复后应执行 bootstrap，实际参数：%q", args)
			}
			info, err = os.Stat(unrelated)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("急救不应改动其他文件权限，实际：%o", info.Mode().Perm())
			}
		})
	}
}

func TestBootstrapRecoveryMissingBinary(t *testing.T) {
	command := recoveryCommandFromHelp(t)
	home := t.TempDir()
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("二进制不存在时必须失败，不能误报已恢复：%s", output)
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "arena")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("急救步骤不应创建或下载二进制：%v", err)
	}
}

func TestBootstrapRecoveryDocsMatchHelp(t *testing.T) {
	command := recoveryCommandFromHelp(t)
	for _, name := range []string{"USAGE.md", "docs/secrets-handling.md"} {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(content), command) {
				t.Errorf("%s 应包含与 bootstrap 帮助一致的急救命令：%s", name, command)
			}
		})
	}
}
