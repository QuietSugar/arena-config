// scan —— 泄露审计（Go 版，替代 scripts/secret-scan.sh）。
//
// 不需要任何密码，纯只读。改动文件后、推送前跑一遍。
// 检查项与 bash 版一一对应：跟踪文件清单 / 工作区明文 / 全历史对象 /
// 不可达对象 / 敏感文件名 / 提交元数据（--remote 顺带查远端）。
//
// 用法：arena scan [--json] [--remote]
// 退出码：0 = 未发现问题；1 = 发现 N 个问题（N 见 --json 的 problems）；2 = 用法错误。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 通用机密特征（公开、与具体环境无关；与 hooks/pre-commit 保持一致）。
const genericPat = `BEGIN (RSA |OPENSSH |EC |DSA |PGP )?PRIVATE KEY|ssh-ed25519 AAAA|ssh-rsa AAAA|ecdsa-sha2-nistp256 AAAA|` +
	`ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|gho_[A-Za-z0-9]{20,}|` +
	`AKIA[0-9A-Z]{16}|aws_secret_access_key|` +
	`xox[baprs]-[A-Za-z0-9-]{10,}|` +
	`-----BEGIN CERTIFICATE-----|` +
	`(PASSWORD|PASSWD|SECRET|TOKEN|APIKEY|API_KEY)[[:space:]]*=[[:space:]]*[A-Za-z0-9]{8,}`

// 规则文件自身含模式串：工作区扫描时对它们只用本环境专属模式（避免自己撞自己）。
// 全历史扫描则直接排除它们在所有提交里的 blob（与 bash 版同口径，注释见下）。
var ruleFiles = []string{"hooks/pre-commit", "scripts/secret-scan.sh", "cmd/arena/scan.go"}

type finding struct {
	Section string `json:"section"`
	Item    string `json:"item"`
	Detail  string `json:"detail"`
}

type scanResult struct {
	PatternsLoaded int       `json:"patterns_loaded"`
	TrackedFiles   int       `json:"tracked_files"`
	Problems       int       `json:"problems"`
	Findings       []finding `json:"findings,omitempty"`
	Committer      string    `json:"committer,omitempty"`
	RemoteRefs     []string  `json:"remote_refs,omitempty"`
}

func runScan(args []string) int {
	jsonOut, remote := false, false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--remote":
			remote = true
		case "-h", "--help":
			fmt.Println("用法：arena scan [--json] [--remote]")
			fmt.Println("泄露审计：工作区 + 全历史 + 不可达对象 + 文件名 + 元数据。退出码：0 无问题 / 1 有问题 / 2 用法错误。")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "scan：未知参数 %s\n", a)
			return 2
		}
	}

	res := &scanResult{}
	var combined *regexp.Regexp // 通用 + 本环境专属
	var localOnly *regexp.Regexp
	{
		local := loadLocalPatterns()
		res.PatternsLoaded = len(local)
		combined = mustCompilePat(genericPat, local)
		if len(local) > 0 {
			localOnly = mustCompilePat("", local)
		}
	}

	bad := func(section, item, detail string) {
		res.Problems++
		res.Findings = append(res.Findings, finding{section, item, detail})
		if !jsonOut {
			mark(cFail, "!!", fmt.Sprintf("%s %s %s", section, item, detail))
		}
	}
	info := func(m string) {
		if !jsonOut {
			mark("\033[36m", "--", m)
		}
	}
	okMsg := func(m string) {
		if !jsonOut {
			mark(cOK, "OK", m)
		}
	}

	root, _ := os.Getwd()
	if gitTop, err := runGit(root, "rev-parse", "--show-toplevel"); err == nil {
		root = strings.TrimSpace(gitTop)
	}

	// ---------- 1. 跟踪文件清单 ----------
	tracked := gitLines(root, "ls-files")
	res.TrackedFiles = len(tracked)
	if jsonOut {
		// 清单在 --json 里只计数；人类输出全量
	} else {
		fmt.Println("== 1. 跟踪的文件（= 外人能看到的全部）==")
		if len(tracked) == 0 {
			info("还没有任何提交")
		} else {
			okMsg(fmt.Sprintf("%d 个文件", len(tracked)))
			for _, f := range tracked {
				fmt.Println("        " + f)
			}
		}
	}

	// ---------- 2. 工作区明文 ----------
	if !jsonOut {
		fmt.Println("== 2. 工作区明文扫描 ==")
	}
	wHit := 0
	for _, f := range tracked {
		p := filepath.Join(root, f)
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 || hasNUL(b) {
			continue // 不存在 / 空 / 二进制（等价 grep -Iq . 的过滤）
		}
		pat := combined
		if isRuleFile(f) && localOnly != nil {
			pat = localOnly
		}
		if pat == nil {
			continue
		}
		if m := matchSample(pat, b); m != "" {
			bad("工作区", f, "含特征："+m)
			wHit++
		}
	}
	if wHit == 0 && !jsonOut {
		okMsg("未发现机密特征")
	}

	// ---------- 3. 全历史对象 ----------
	if !jsonOut {
		fmt.Println("== 3. 全历史对象扫描 ==")
	}
	skipBlobs := map[string]bool{}
	for _, c := range gitLines(root, "rev-list", "--all") {
		for _, pf := range ruleFiles {
			if out, err := runGit(root, "rev-parse", c+":"+pf); err == nil {
				skipBlobs[strings.TrimSpace(out)] = true
			}
		}
	}
	total, hHit := 0, 0
	for _, line := range gitLines(root, "cat-file", "--batch-all-objects", "--batch-check") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "blob" {
			continue
		}
		sha := fields[0]
		if skipBlobs[sha] {
			continue
		}
		total++
		if out, err := runGit(root, "cat-file", "-p", sha); err == nil {
			if m := matchSample(combined, []byte(out)); m != "" {
				where := ""
				if wl, err := runGit(root, "log", "--all", "--oneline", "--find-object="+sha); err == nil {
					wl = strings.TrimSpace(wl)
					if i := strings.Index(wl, "\n"); i >= 0 {
						wl = wl[:i]
					}
					where = wl
				}
				bad("历史对象", sha[:12], "含机密特征（"+where+"）")
				hHit++
			}
		}
	}
	if hHit == 0 && !jsonOut {
		okMsg(fmt.Sprintf("扫描 %d 个历史对象，未发现机密特征", total))
	}

	// ---------- 4. 不可达 / 悬空对象 ----------
	if !jsonOut {
		fmt.Println("== 4. 不可达 / 悬空对象 ==")
	}
	unreach := gitLines(root, "fsck", "--unreachable", "--dangling")
	if len(unreach) == 0 {
		if !jsonOut {
			okMsg("无残留")
		}
	} else {
		danger := 0
		for _, line := range unreach {
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			sha := fields[2]
			if out, err := runGit(root, "cat-file", "-p", sha); err == nil {
				if m := matchSample(combined, []byte(out)); m != "" {
					size, _ := runGit(root, "cat-file", "-s", sha)
					bad("危险 blob", sha[:12], fmt.Sprintf("（%s 字节）含机密特征", strings.TrimSpace(size)))
					danger++
				}
			}
		}
		if danger == 0 && !jsonOut {
			info(fmt.Sprintf("%d 个残留对象，未见机密特征（多为历史改写留下的空壳）", len(unreach)))
		}
		if !jsonOut {
			fmt.Println()
			fmt.Println("    清理（会丢弃这些残留对象）：")
			fmt.Println("        git reflog expire --expire=now --all && git gc --prune=now")
			fmt.Println("    ⚠️ 残留只存在于本地，不会随 push 传播；但本机磁盘上确实还留着内容。")
		}
	}

	// ---------- 5. 敏感文件名 ----------
	if !jsonOut {
		fmt.Println("== 5. 敏感文件名 ==")
	}
	fHit := 0
	for _, f := range tracked {
		if sensitiveName(filepath.Base(f)) {
			bad("敏感文件名", f, "不允许跟踪")
			fHit++
		}
	}
	if fHit == 0 && !jsonOut {
		okMsg("无")
	}

	// ---------- 6. 提交元数据 ----------
	if !jsonOut {
		fmt.Println("== 6. 提交元数据 ==")
	}
	mHit := 0
	for _, line := range gitLines(root, "log", "--all", "--format=%h|%ad|%cd", "--date=iso-strict") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) == 3 && parts[1] != parts[2] {
			bad("元数据", parts[0], "作者时间与提交者时间不一致（可能改过历史）")
			mHit++
		}
	}
	if mHit == 0 && !jsonOut {
		okMsg("时间一致，未见重写痕迹")
	}
	if c, err := runGit(root, "log", "-1", "--format=%an <%ae>"); err == nil {
		res.Committer = strings.TrimSpace(c)
		if !jsonOut {
			info("提交者：" + res.Committer)
		}
	}

	// ---------- 7. 远端 ----------
	if remote {
		if !jsonOut {
			fmt.Println("== 7. 远端引用 ==")
		}
		if _, err := runGit(root, "remote", "get-url", "origin"); err != nil {
			info("未配置 origin")
		} else {
			if _, err := runGit(root, "fetch", "--quiet", "origin"); err != nil {
				bad("远端", "fetch", "失败（检查凭据 / 网络）")
			}
			for _, b := range gitLines(root, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin") {
				ahead, _ := runGit(root, "rev-list", "--count", b+"..HEAD")
				res.RemoteRefs = append(res.RemoteRefs, b)
				info(fmt.Sprintf("%s（本地领先 %s 个提交）", b, strings.TrimSpace(ahead)))
			}
		}
	}

	if jsonOut {
		emitJSON(res)
	} else {
		fmt.Println()
		fmt.Println("==================== 小结 ====================")
		if res.Problems == 0 {
			fmt.Println("  ✅ 未发现问题")
		} else {
			fmt.Printf("  ❌ 发现 %d 个问题（见上面的 [!!]）\n", res.Problems)
		}
	}
	if res.Problems > 0 {
		return 1
	}
	return 0
}

func loadLocalPatterns() []string {
	b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".arena-secrets", "secret-patterns"))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

func mustCompilePat(generic string, locals []string) *regexp.Regexp {
	// 环境专属模式来自 ERE；RE2 语法差异极小（本项目用到的转义均兼容）。
	parts := []string{}
	if generic != "" {
		parts = append(parts, generic)
	}
	parts = append(parts, locals...)
	return regexp.MustCompile(strings.Join(parts, "|"))
}

func isRuleFile(f string) bool {
	for _, r := range ruleFiles {
		if f == r {
			return true
		}
	}
	return false
}

func hasNUL(b []byte) bool {
	return strings.IndexByte(string(b), 0) >= 0
}

// matchSample 返回命中样本（最多 3 个去重，空格连接）；无命中返回空串。
func matchSample(re *regexp.Regexp, b []byte) string {
	ms := re.FindAll(b, -1)
	if len(ms) == 0 {
		return ""
	}
	set := map[string]bool{}
	var out []string
	for _, m := range ms {
		s := string(m)
		if !set[s] {
			set[s] = true
			out = append(out, s)
		}
		if len(out) == 3 {
			break
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func sensitiveName(base string) bool {
	switch base {
	case "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", ".netrc", ".git-credentials", "secrets.json", "AGENTS.local.md":
		return true
	}
	for _, suf := range []string{".pem", ".key", ".p12", ".pfx", ".local.md"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	return strings.Contains(base, "credentials")
}

func gitLines(dir string, args ...string) []string {
	out, err := runGit(dir, args...)
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
