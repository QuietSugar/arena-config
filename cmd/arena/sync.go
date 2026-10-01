// sync —— 在沙箱与目标机之间同步代码树、执行远端脚本（Go 版，替代 scripts/dcupsync）。
//
// 为什么要用它：直接用 ssh 往远端写文件，嵌套引号会被两层 shell 各解析一次，
// 很容易出错。正确做法是「本地写好文件 → 传上去执行」，本子命令把这条路径固化。
//
// 用法：
//
//	arena sync pull   <远端路径> [本地目录]    远端 → 本地（改文件前先拉）
//	arena sync push   [--delete] <远端路径> [本地目录]
//	arena sync diff   <远端路径> [本地目录]    干跑，只看会改什么（推送前必看）
//	arena sync status <远端路径> [本地目录]    两边文件数/大小对比
//	arena sync run    <本地脚本> [参数...]     把本地脚本推到远端执行（免引号地狱）
//	arena sync ssh    <命令...>                在远端执行一条命令
//
// 连接：ssh 别名取自 env.sh（DCUPSYNC_HOST > ARENA_TARGET_ALIAS > target）。
// rsync 参数语义是踩坑固化结论，改动前先读 docs/file-sync-workflow.md 第 2 节；
// rsync -e 里绝不带主机名。
//
// 退出码：0 = 成功；1 = 失败（连不上/路径不存在/rsync 报错）；2 = 用法错误。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var syncExcludes = []string{
	"--exclude=logs/", "--exclude=config/session/", "--exclude=__pycache__/",
	"--exclude=*.pyc", "--exclude=.git/", "--exclude=*.swp", "--exclude=*~",
}

var syncRsyncOpts = []string{
	"-a", "--no-owner", "--no-group", "--no-perms", "--checksum",
	"--human-readable", "--itemize-changes",
}

func runSync(args []string) int {
	if len(args) == 0 {
		syncUsage()
		return 2
	}
	env := parseExportsFile()
	sshHost := env["DCUPSYNC_HOST"]
	if sshHost == "" {
		sshHost = env["ARENA_TARGET_ALIAS"]
	}
	if sshHost == "" {
		sshHost = "target"
	}
	rsyncRsh := os.Getenv("DCUPSYNC_RSYNC_RSH")
	if rsyncRsh == "" {
		rsyncRsh = "ssh"
	}
	sc := &syncer{host: sshHost, remoteRoot: env["ARENA_DEFAULT_REMOTE_ROOT"], rsh: rsyncRsh}

	switch args[0] {
	case "pull":
		return sc.pull(args[1:])
	case "push":
		rest := args[1:]
		delete := false
		if len(rest) > 0 && rest[0] == "--delete" {
			delete = true
			rest = rest[1:]
		}
		return sc.push(rest, delete)
	case "diff":
		return sc.diff(args[1:])
	case "status":
		return sc.status(args[1:])
	case "run":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "sync run：缺少本地脚本参数")
			return 2
		}
		return sc.runScript(args[1], args[2:])
	case "ssh":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "sync ssh：缺少命令参数")
			return 2
		}
		if err := sc.preflight(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		cmd := exec.Command("ssh", sc.host, strings.Join(args[1:], " "))
		cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
			return 1
		}
		return 0
	case "-h", "--help":
		syncUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "sync：未知子命令 %s\n", args[0])
		syncUsage()
		return 2
	}
}

func syncUsage() {
	fmt.Println(`用法：arena sync <子命令> [参数]

  pull   <远端路径> [本地目录]    远端 → 本地（改文件前先拉）
  push   [--delete] <远端路径> [本地目录]
  diff   <远端路径> [本地目录]    干跑，只看会改什么（推送前必看）
  status <远端路径> [本地目录]    两边文件数/大小对比
  run    <本地脚本> [参数...]     把本地脚本推到远端执行（免引号地狱）
  ssh    <命令...>                在远端执行一条命令

本地目录省略时默认 ~/sync/<远端目录名>；远端路径省略时用 env.sh 的 default_remote_root。`)
}

type syncer struct {
	host       string
	remoteRoot string
	rsh        string
}

// pathPair 解析 [远端路径] [本地目录]；远端省略时退回 default_remote_root。
func (sc *syncer) pathPair(args []string) (remote, local string, err error) {
	switch len(args) {
	case 0:
		if sc.remoteRoot == "" {
			return "", "", fmt.Errorf("缺少远端路径参数（也可在 secrets.json 里配 default_remote_root）")
		}
		remote = sc.remoteRoot
	case 1, 2:
		remote = args[0]
	default:
		return "", "", fmt.Errorf("参数太多：%v", args)
	}
	remote = strings.TrimRight(remote, "/")
	if len(args) == 2 {
		local = strings.TrimRight(args[1], "/")
	} else {
		local = filepath.Join(os.Getenv("HOME"), "sync", filepath.Base(remote))
	}
	return remote, local, nil
}

func (sc *syncer) preflight() error {
	cmd := exec.Command("ssh", "-o", "ConnectTimeout=60", sc.host, "echo ok")
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "ok") {
		return fmt.Errorf(`连不上目标机。
  先运行 arena check —— 它会打印 ssh 原始报错，并给出该对用户说的话。
  若是反向隧道断了，按 AGENTS.local.md 里的命令让用户在内网机器重建；不要自己找绕路方案。`)
	}
	return nil
}

func (sc *syncer) rsyncBase() []string {
	return append(append([]string{}, syncRsyncOpts...), syncExcludes...)
}

func (sc *syncer) pull(args []string) int {
	r, l, err := sc.pathPair(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := sc.preflight(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(l, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf(">>> 拉取 %s/  →  %s/\n", r, l)
	cmd := exec.Command("rsync", append(sc.rsyncBase(), "-e", sc.rsh, sc.host+":"+r+"/", l+"/")...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return 1
	}
	fmt.Printf(">>> 完成。本地副本：%s\n", l)
	return 0
}

func (sc *syncer) push(args []string, delete bool) int {
	r, l, err := sc.pathPair(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := sc.preflight(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if st, err := os.Stat(l); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "✗ 本地目录不存在：%s（先 arena sync pull）\n", l)
		return 1
	}
	opts := sc.rsyncBase()
	if delete {
		opts = append(opts, "--delete")
		fmt.Println("!!! --delete 已启用：远端会删除本地不存在的文件")
		fmt.Println("!!! 若远端刚跑过构建（会重写 option/session/build 等文件），先 pull 再 push")
	}
	fmt.Printf(">>> 推送 %s/  →  %s/\n", l, r)
	cmd := exec.Command("rsync", append(opts, "-e", sc.rsh, l+"/", sc.host+":"+r+"/")...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return 1
	}
	fmt.Printf(">>> 完成。远端：%s\n", r)
	return 0
}

func (sc *syncer) diff(args []string) int {
	r, l, err := sc.pathPair(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := sc.preflight(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if st, err := os.Stat(l); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "✗ 本地目录不存在：%s\n", l)
		return 1
	}
	fmt.Println(">>> 本地相对远端的差异（< 行表示 push 会写过去的改动）")
	cmd := exec.Command("rsync", append(sc.rsyncBase(), "--dry-run", "-e", sc.rsh, l+"/", sc.host+":"+r+"/")...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return 1
	}
	fmt.Println(">>> 以上即 push 将执行的操作。若出现 *deleting 行，说明用了 --delete 才会删。")
	return 0
}

// status 两边用同一套排除口径统计文件数与大小。
func (sc *syncer) status(args []string) int {
	r, l, err := sc.pathPair(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := sc.preflight(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	const findArgs = "-type f -not -path '*/logs/*' -not -path '*/config/session/*' -not -path '*/.git/*' -not -name '*.pyc'"
	fmt.Printf("=== 本地 %s ===\n", l)
	if st, err := os.Stat(l); err == nil && st.IsDir() {
		n, _ := runShell("bash", "-c", fmt.Sprintf("find %s %s | wc -l", shellescape(l), findArgs))
		sz, _ := runShell("bash", "-c", fmt.Sprintf("du -sh --exclude=logs --exclude=config/session %s 2>/dev/null | awk '{print $1}'", shellescape(l)))
		fmt.Printf("文件数: %s\n大小:   %s\n", strings.TrimSpace(n), strings.TrimSpace(sz))
	} else {
		fmt.Println("(尚未 pull 过)")
	}
	fmt.Println()
	fmt.Printf("=== 远端 %s（同一排除口径）===\n", r)
	remote := fmt.Sprintf(`bash -lc "echo 文件数: \$(find %s %s | wc -l); du -sh --exclude=logs --exclude=config/session %s 2>/dev/null | awk '{print \"大小:   \"\$1}'"`, shellescape(r), findArgs, shellescape(r))
	out, err := exec.Command("ssh", sc.host, remote).Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "远端统计失败：", err)
		return 1
	}
	fmt.Print(string(out))
	return 0
}

// runScript 用 stdin 把本地脚本送到远端 /tmp 执行（内容不经过任何一层 shell 解析）。
func (sc *syncer) runScript(script string, scriptArgs []string) int {
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintf(os.Stderr, "✗ 本地脚本不存在：%s\n", script)
		return 1
	}
	if err := sc.preflight(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	base := filepath.Base(script)
	rtmp := fmt.Sprintf("/tmp/.arena_sync_run_%d_%s", os.Getpid(), base)
	up := exec.Command("ssh", sc.host, "cat > "+shellescape(rtmp)+" && chmod +x "+shellescape(rtmp))
	content, err := os.ReadFile(script)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	up.Stdin = strings.NewReader(string(content))
	up.Stdout, up.Stderr = os.Stdout, os.Stderr
	if err := up.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "✗ 上传脚本失败")
		return 1
	}
	fmt.Printf(">>> 远端执行：%s（参数：%s）\n", base, strings.Join(scriptArgs, " "))
	remote := fmt.Sprintf("bash %s %s; rc=$?; rm -f %s; exit $rc", shellescape(rtmp), strings.Join(scriptArgs, " "), shellescape(rtmp))
	run := exec.Command("ssh", sc.host, remote)
	run.Stdout, run.Stderr, run.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := run.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

func runShell(name string, args ...string) (string, error) {
	b, err := exec.Command(name, args...).Output()
	return string(b), err
}

// shellescape 单引号包裹（值里含单引号时换双引号转义，与 bashQuote 同策略）。
func shellescape(s string) string { return bashQuote(s) }
