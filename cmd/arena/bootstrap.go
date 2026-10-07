// bootstrap —— 沙箱被回收后的环境自检与自动修复（Go 版，替代 workspace-bootstrap.sh）。
//
// 覆盖：git 配置（.git/config 会被沙箱快照丢弃）、密钥权限与配对、
// arena 二进制自我更新（GitHub Release）、rsync 等本地工具、连通性检查。
//
// 用法：arena bootstrap [--json] [--no-self-update]
// 退出码：0 = 全部就绪；1 = 有需要处理的问题；2 = 用法错误。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/QuietSugar/arena-config/internal/secrets"
)

type bootItem struct {
	Status string `json:"status"` // ok | warn | fail
	Msg    string `json:"msg"`
}

type bootResult struct {
	Problems int        `json:"problems"`
	Items    []bootItem `json:"items"`
}

func runBootstrap(args []string) int {
	jsonOut, noSelfUpdate := false, false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--no-self-update":
			noSelfUpdate = true
		case "-h", "--help":
			fmt.Println("用法：arena bootstrap [--json] [--no-self-update]")
			fmt.Println("自检并修复环境。退出码：0 就绪 / 1 有问题 / 2 用法错误。")
			fmt.Println()
			fmt.Println("快照恢复急救：若 arena 自身报 Permission denied，先在 shell 恢复执行位，再运行 bootstrap：")
			fmt.Println(`  chmod 755 "$HOME/bin/arena" && "$HOME/bin/arena" bootstrap`)
			fmt.Println("二进制不可执行时无法自行修复，也无法显示本帮助；可直接查阅 USAGE.md「快照恢复急救」。")
			fmt.Println("自定义安装目录：将命令中两处路径换成实际 arena 路径，并保留双引号。")
			fmt.Println("chmod 不依赖网络；随后 bootstrap 仍会自更新和检查连通性，追加 --no-self-update 仅跳过自更新。")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "bootstrap：未知参数 %s\n", a)
			return 2
		}
	}

	res := &bootResult{}
	section := func(name string) {
		if !jsonOut {
			fmt.Printf("== %s ==\n", name)
		}
	}
	add := func(status, msg string) {
		res.Items = append(res.Items, bootItem{status, msg})
		if status == "fail" {
			res.Problems++
		}
		if !jsonOut {
			color := map[string]string{"ok": cOK, "warn": cWarn, "fail": cFail}[status]
			tag := map[string]string{"ok": "OK", "warn": "WARN", "fail": "FAIL"}[status]
			mark(color, tag, msg)
		}
	}
	env := parseExportsFile()
	repoRoot := resolveRepoRoot(env["ARENA_REPO_ROOT"])

	// 0. git 配置
	section("0. 仓库 git 配置（快照会丢弃 .git/config）")
	if repoRoot == "" {
		add("warn", "不在仓库内（缺 AGENTS.md+hooks/）→ 跳过 git 配置修复")
	} else {
		if _, err := runGit(repoRoot, "config", "remote.origin.url"); err != nil {
			if origin := env["ARENA_ORIGIN_URL"]; origin != "" {
				cur, _ := runGit(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
				branch := strings.TrimSpace(cur)
				if branch == "" {
					branch = "main"
				}
				_, _ = runGit(repoRoot, "config", "remote.origin.url", origin)
				_, _ = runGit(repoRoot, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
				_, _ = runGit(repoRoot, "config", "branch."+branch+".remote", "origin")
				_, _ = runGit(repoRoot, "config", "branch."+branch+".merge", "refs/heads/"+branch)
				add("ok", "远端 origin 已重建（"+branch+"）")
			} else {
				add("warn", "无法重建 origin：env.sh 里没有 ARENA_ORIGIN_URL（先运行 arena import）")
			}
		}
		if out, _ := runGit(repoRoot, "config", "core.hooksPath"); strings.TrimSpace(out) != "hooks" {
			if _, err := runGit(repoRoot, "config", "core.hooksPath", "hooks"); err == nil {
				add("ok", "core.hooksPath → hooks（钩子已重新生效）")
			}
		} else {
			add("ok", "git 配置与钩子正常")
		}
		hook := filepath.Join(repoRoot, "hooks", "pre-commit")
		if st, err := os.Stat(hook); err == nil && st.Mode()&0o111 == 0 {
			_ = os.Chmod(hook, 0o755)
			add("ok", "补执行位：hooks/pre-commit")
		}
	}

	// 1. SSH 密钥
	section("1. SSH 配置与密钥")
	sshDir := filepath.Join(os.Getenv("HOME"), ".ssh")
	// 连接复用的 socket 目录：快照后可能丢失，ssh 不会自己建（承接远端 476b101 的修复意图）
	cmDir := filepath.Join(sshDir, "cm")
	mustMkdirAll(cmDir, 0o700)
	cleanStaleSockets(cmDir)
	keyPath := resolveIdentityFile(sshDir, env["ARENA_TARGET_ALIAS"])
	if keyPath == "" {
		add("fail", "私钥不存在 → 请把 secrets.json 交给用户或从备份恢复，然后运行 arena import")
	} else {
		_ = os.Chmod(sshDir, 0o700)
		entries, _ := os.ReadDir(sshDir)
		for _, e := range entries {
			st, err := e.Info()
			if err != nil || st.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".pub") {
				_ = os.Chmod(filepath.Join(sshDir, e.Name()), 0o644)
			} else {
				_ = os.Chmod(filepath.Join(sshDir, e.Name()), 0o600)
			}
		}
		add("ok", "私钥权限 600")
		if exp := env["ARENA_EXPECTED_PUBKEY"]; exp != "" {
			if err := secrets.CheckExpectedPubkey(keyPath, exp); err != nil {
				add("fail", err.Error())
			} else {
				add("ok", "私钥与预期公钥匹配（"+filepath.Base(keyPath)+"）")
			}
		}
	}

	// 2. 自更新
	section("2. arena 自更新（GitHub Release）")
	if noSelfUpdate {
		add("warn", "--no-self-update：跳过")
	} else if err := selfUpdate(); err != nil {
		add("warn", "自更新不可用："+err.Error())
	} else {
		add("ok", "arena 已是最新（"+version+"）")
	}

	// 3. 本地工具
	section("3. 本地工具")
	if _, err := exec.LookPath("rsync"); err == nil {
		add("ok", "rsync 在位")
	} else {
		add("warn", "rsync 缺失 → 尝试安装")
		if err := tryInstallRsync(); err != nil {
			add("fail", "rsync 安装失败（arena sync 需要它）："+err.Error())
		} else {
			add("ok", "rsync 安装完成")
		}
	}
	restored := 0
	for _, pat := range []string{filepath.Join(os.Getenv("HOME"), "bin", "*"), filepath.Join(repoRootOrEmpty(repoRoot), "scripts", "*.sh"), filepath.Join(repoRootOrEmpty(repoRoot), "hooks", "*")} {
		if pat == "" {
			continue
		}
		for _, f := range globOrNil(pat) {
			if st, err := os.Stat(f); err == nil && !st.IsDir() && st.Mode()&0o111 == 0 {
				if os.Chmod(f, 0o755) == nil {
					restored++
				}
			}
		}
	}
	if restored > 0 {
		add("ok", fmt.Sprintf("恢复 %d 个文件的可执行位", restored))
	} else {
		add("ok", "可执行位正常")
	}

	// 4. 连通
	section("4. 连通目标机")
	if rc := runCheck(nil); rc != 0 {
		res.Problems++
	}

	if jsonOut {
		emitJSON(res)
	} else {
		fmt.Println()
		if res.Problems == 0 {
			fmt.Println("✅ 环境就绪。")
		} else {
			fmt.Printf("⚠️  有 %d 项需要处理（见上面 FAIL）\n", res.Problems)
		}
	}
	if res.Problems > 0 {
		return 1
	}
	return 0
}

func repoRootOrEmpty(root string) string {
	if root == "" {
		return os.Getenv("HOME") + "/.nonexistent"
	}
	return root
}

func globOrNil(pat string) []string {
	m, _ := filepath.Glob(pat)
	return m
}

func lookPath(name string) (bool, error) {
	_, err := exec.LookPath(name)
	return err == nil, err
}

func parseExportsFile() map[string]string {
	b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".arena-secrets", "env.sh"))
	if err != nil {
		return map[string]string{}
	}
	return parseExports(string(b))
}

// resolveIdentityFile 用 ssh -G 找别名实际使用的私钥；找不到退回 ~/.ssh/id_ed25519。
// 这比盲猜文件名聪明：合并式布局（密钥改名 .arena）也能配对到正确的钥匙。
func resolveIdentityFile(sshDir, alias string) string {
	if alias == "" {
		alias = "target"
	}
	if out, err := execSSHG(alias); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if v, found := strings.CutPrefix(l, "identityfile "); found {
				p := strings.TrimSpace(strings.Split(v, " ")[0])
				p = strings.Replace(p, "~", os.Getenv("HOME"), 1)
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p
				}
			}
		}
	}
	fallback := filepath.Join(sshDir, "id_ed25519")
	if _, err := os.Stat(fallback); err == nil {
		return fallback
	}
	return ""
}

func execSSHG(alias string) (string, error) {
	b, err := exec.Command("ssh", "-G", alias).Output()
	return string(b), err
}

func syncTool(repoRoot, name string) error {
	src := filepath.Join(repoRoot, "scripts", name)
	if _, err := os.Stat(src); err != nil {
		return nil // 工具已退役（如 dcupsync 被 arena sync 取代），跳过
	}
	dst := filepath.Join(os.Getenv("HOME"), "bin", name)
	sb, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if db, err := os.ReadFile(dst); err == nil && string(db) == string(sb) {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		_ = os.Rename(dst, dst+".bak")
	}
	return os.WriteFile(dst, sb, 0o755)
}
