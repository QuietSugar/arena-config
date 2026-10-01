// import —— 把一份 secrets.json 变成可用环境（Go 版，替代 scripts/arena-import.sh）。
//
// 职责与旧 bash 版一致：写密钥（带配对校验）、渲染 ssh config、生成 known_hosts
// （主机名哈希）、env.sh / secret-patterns / AGENTS.local.md、启用 pre-commit 钩子、
// 自安装到 ~/bin。模板内嵌于二进制，无需克隆仓库。
//
// 用法：
//
//	arena import                      # 读 ~/.arena-secrets/secrets.json
//	arena import --from-json FILE
//	echo '<json>' | arena import --from-stdin
//	ARENA_SECRETS='<json>' arena import
//	arena import --check              # 导入后顺带跑连通性检查
//	arena import --print-schema       # 打印 JSON 结构说明
//	arena import --repo-root DIR      # 指定仓库根（渲染 AGENTS.local.md、启用钩子）；
//	                                  # 缺省在 cwd 长得像本仓库时用 cwd，否则跳过并提示
//
// 退出码：0 = 成功；1 = 失败（JSON/校验/渲染错误）；2 = 用法错误。
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuietSugar/arena-config/internal/secrets"
)

const schemaDoc = `secrets.json 结构（arena-secrets/1）

{
  "schema": "arena-secrets/1",
  "ssh_keys":  [ { "name": "id_ed25519", "kind": "private|public",
                  "mode": "600", "data_b64": "<base64>" } ],
  "hosts": {
    "target":  { "alias", "hostname", "port", "user", "host_key_alias", "lan_ip", "key" },
    "transit": { "alias", "host", "user", "key" },
    "github":  { "alias", "ssh_host", "port", "user", "key", "clone_url" }
  },
  "host_keys": [ { "alias", "key_type", "key_b64", "fingerprint" } ],
  "tooling": {
    "secret_patterns":     [ "<ERE>", ... ],
    "expected_pubkey":     "<base64 of ed25519 public key blob>",
    "default_remote_root": "/path/on/target"
  }
}

编码规则：
  * 含换行 / 需要字节精确的内容（私钥、公钥、主机公钥）→ base64，放 data_b64 / key_b64
  * 标量（IP、域名、端口、用户名、路径）→ 明文
  * fingerprint 是主机公钥的 SHA256 指纹，仅用于*校验*；
    生成 known_hosts 必须用 key_b64（指纹是单向哈希，无法反推出公钥）

⚠️ base64 不是加密，只保证字节精确。整个 JSON 是明文，泄漏即全部泄漏。
历史 JSON 里的 wstunnel 字段已随主通道移除，会被静默忽略。`

func runImport(args []string) int {
	var fromJSON, repoRoot string
	var fromStdin, doCheck bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from-json":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "--from-json 需要文件路径参数")
				return 2
			}
			fromJSON = args[i]
		case "--from-stdin":
			fromStdin = true
		case "--check":
			doCheck = true
		case "--print-schema":
			fmt.Println(schemaDoc)
			return 0
		case "--repo-root":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "--repo-root 需要目录参数")
				return 2
			}
			repoRoot = args[i]
		case "-h", "--help":
			fmt.Println("用法：arena import [--from-json FILE | --from-stdin] [--check] [--repo-root DIR]")
			fmt.Println("退出码：0 成功 / 1 失败 / 2 用法错误。")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "import：未知参数 %s\n", args[i])
			return 2
		}
	}

	data, err := readSecrets(fromJSON, fromStdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] %v\n", err)
		return 1
	}
	s, err := secrets.Parse(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] %v\n", err)
		return 1
	}
	ok := func(m string) { mark(cOK, "OK", m) }

	// 1) 密钥：先全部解码+校验，通过后才落盘
	keys, err := s.DecodeKeys()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] %v\n", err)
		return 1
	}
	sshDir := filepath.Join(os.Getenv("HOME"), ".ssh")
	mustMkdirAll(sshDir, 0o700)
	for _, k := range keys {
		p := filepath.Join(sshDir, k.Name)
		if err := os.WriteFile(p, k.Data, os.FileMode(k.Mode)); err != nil {
			fmt.Fprintf(os.Stderr, "[FAIL] 写 %s 失败：%v\n", p, err)
			return 1
		}
	}
	ok(fmt.Sprintf("写入 %d 个密钥文件", len(keys)))
	provided := map[string]bool{}
	for _, sk := range s.SSHKeys {
		provided[sk.Name] = true
	}
	for _, sk := range s.SSHKeys {
		if sk.Kind == "private" && provided[sk.Name+".pub"] {
			ok(fmt.Sprintf("%s ↔ %s.pub 配对正确", sk.Name, sk.Name))
		}
	}

	// 2) ssh config
	cfg, err := render(sshConfigTmpl, sshConfigSubs(s, sshDir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] 渲染 ssh config：%v\n", err)
		return 1
	}
	write600(filepath.Join(sshDir, "config"), cfg)
	ok("渲染 ~/.ssh/config")

	// 3) known_hosts（主机名哈希）
	entries, err := s.BuildEntries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] %v\n", err)
		return 1
	}
	if len(entries) == 0 {
		mark(cWarn, "WARN", "JSON 未提供 host_keys → 跳过 known_hosts（StrictHostKeyChecking yes 将拒绝连接）")
	} else {
		write600(filepath.Join(sshDir, "known_hosts"), secrets.Render(entries))
		ok(fmt.Sprintf("生成 known_hosts（%d 台主机，主机名已哈希）", len(entries)))
	}

	// 4) 派生配置
	secDir := filepath.Join(os.Getenv("HOME"), ".arena-secrets")
	mustMkdirAll(secDir, 0o700)
	write600(filepath.Join(secDir, "secrets.json"), string(data))
	write600(filepath.Join(secDir, "env.sh"), envSh(s, repoRoot))
	var patterns []string
	for _, p := range s.Tooling.SecretPatterns {
		if p != "" {
			patterns = append(patterns, p)
		}
	}
	write600(filepath.Join(secDir, "secret-patterns"),
		"# 由 arena import 生成 —— 每行一个 ERE，供 hooks/pre-commit 与 arena scan 读取\n"+strings.Join(patterns, "\n")+"\n")
	ok("写入 ~/.arena-secrets/（secrets.json / env.sh / secret-patterns）")

	// 5) 仓库相关：AGENTS.local.md + 钩子（需 --repo-root 或 cwd 可识别）
	root := resolveRepoRoot(repoRoot)
	if root == "" {
		mark(cWarn, "WARN", "未指定 --repo-root 且当前目录不像本仓库 → 跳过 AGENTS.local.md 渲染与钩子启用")
	} else {
		ag, err := render(agentsLocalTmpl, agentsLocalSubs(s, sshDir))
		if err != nil {
			fmt.Fprintf(os.Stderr, "[FAIL] 渲染 AGENTS.local.md：%v\n", err)
			return 1
		}
		write600(filepath.Join(root, "AGENTS.local.md"), ag)
		ok("渲染 AGENTS.local.md（含真实主机信息，已被 gitignore）")
		hook := filepath.Join(root, "hooks", "pre-commit")
		if _, err := os.Stat(hook); err == nil {
			_ = os.Chmod(hook, 0o755)
			if out, err := runGit(root, "config", "core.hooksPath", "hooks"); err != nil {
				_ = out
				mark(cWarn, "WARN", "设置 core.hooksPath 失败（在仓库外？）")
			} else {
				ok("pre-commit 钩子已启用（core.hooksPath=hooks）")
			}
		}
	}

	// 6) ~/.ssh 收尾：cm 目录、陈旧 socket、权限归一
	cmDir := filepath.Join(sshDir, "cm")
	mustMkdirAll(cmDir, 0o700)
	cleanStaleSockets(cmDir)
	normalizeKeyPerms(sshDir)

	// 7) .bashrc 快捷变量（幂等）
	writeBashrcBlock()

	// 8) 自安装到 ~/bin/arena
	if err := selfInstall(); err != nil {
		mark(cWarn, "WARN", fmt.Sprintf("自安装到 ~/bin 失败：%v", err))
	} else {
		ok("已安装 arena → ~/bin/")
	}

	fmt.Println()
	fmt.Println("✅ 导入完成。目标机：ssh " + s.Hosts.Target.Alias)
	if root != "" {
		fmt.Println("   环境规则：" + filepath.Join(root, "AGENTS.local.md") + "（含真实主机信息，已被 gitignore）")
	}
	if doCheck {
		fmt.Println()
		fmt.Println("== 连通性检查 ==")
		return runCheck(nil)
	}
	return 0
}

// readSecrets 按优先级取 JSON：--from-json > --from-stdin > $ARENA_SECRETS > 默认文件。
func readSecrets(fromJSON string, fromStdin bool) ([]byte, error) {
	switch {
	case fromJSON != "":
		b, err := os.ReadFile(fromJSON)
		if err != nil {
			return nil, fmt.Errorf("找不到 secrets.json：%s", fromJSON)
		}
		return b, nil
	case fromStdin:
		b, err := io.ReadAll(os.Stdin)
		if err != nil || len(b) == 0 {
			return nil, fmt.Errorf("标准输入为空或读取失败")
		}
		return b, nil
	case os.Getenv("ARENA_SECRETS") != "":
		return []byte(os.Getenv("ARENA_SECRETS")), nil
	default:
		p := filepath.Join(os.Getenv("HOME"), ".arena-secrets", "secrets.json")
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf(`找不到 secrets.json：%s

请把私密数据 JSON 交给本工具，三选一：
  1) 保存为 %s（权限 600）
  2) arena import --from-json /path/to/secrets.json
  3) echo '<json>' | arena import --from-stdin

格式说明：arena import --print-schema`, p, p)
		}
		return b, nil
	}
}

func sshConfigSubs(s *secrets.Secrets, sshDir string) []kv {
	t, tr, gh := s.Hosts.Target, s.Hosts.Transit, s.Hosts.Github
	return []kv{
		{"SSH_DIR", sshDir},
		{"TARGET_ALIAS", t.Alias},
		{"TARGET_HOSTNAME", t.Hostname},
		{"TARGET_PORT", itoa(t.Port)},
		{"TARGET_USER", t.User},
		{"TARGET_HOST_KEY_ALIAS", orDefault(t.HostKeyAlias, t.Hostname)},
		{"TARGET_KEY", t.Key},
		{"TRANSIT_ALIAS", tr.Alias},
		{"TRANSIT_HOST", tr.Host},
		{"TRANSIT_USER", tr.User},
		{"TRANSIT_KEY", tr.Key},
		{"GITHUB_ALIAS", gh.Alias},
		{"GITHUB_SSH_HOST", gh.SSHHost},
		{"GITHUB_PORT", itoa(gh.Port)},
		{"GITHUB_USER", gh.User},
		{"GITHUB_KEY", gh.Key},
	}
}

func agentsLocalSubs(s *secrets.Secrets, sshDir string) []kv {
	base := filepath.Base(strings.TrimRight(s.Tooling.DefaultRemoteRoot, "/"))
	if base == "." || base == "/" || base == "" {
		base = "sync"
	}
	subs := append(sshConfigSubs(s, sshDir),
		kv{"TARGET_LAN_IP", orDefault(s.Hosts.Target.LANIP, "-")},
		kv{"DEFAULT_REMOTE_ROOT", orDefault(s.Tooling.DefaultRemoteRoot, "-")},
		kv{"SYNC_BASENAME", base},
	)
	return subs
}

func envSh(s *secrets.Secrets, repoRoot string) string {
	lines := []string{
		"# 由 arena import 生成 —— 供 shell 里 source",
		"export ARENA_TARGET_ALIAS=" + bashQuote(s.Hosts.Target.Alias),
		"export ARENA_TRANSIT_HOST=" + bashQuote(s.Hosts.Transit.Host),
		"export DCUPSYNC_HOST=" + bashQuote(s.Hosts.Target.Alias),
		"export ARENA_DEFAULT_REMOTE_ROOT=" + bashQuote(s.Tooling.DefaultRemoteRoot),
		"export ARENA_ORIGIN_URL=" + bashQuote(s.Hosts.Github.CloneURL),
		"export ARENA_EXPECTED_PUBKEY=" + bashQuote(s.Tooling.ExpectedPubkey),
	}
	if root := resolveRepoRoot(repoRoot); root != "" {
		lines = append(lines, "export ARENA_REPO_ROOT="+bashQuote(root))
	}
	return strings.Join(lines, "\n") + "\n"
}

// bashQuote 单引号包裹；值里含单引号时退化为双引号加转义。
func bashQuote(v string) string {
	if !strings.Contains(v, "'") {
		return "'" + v + "'"
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
}
