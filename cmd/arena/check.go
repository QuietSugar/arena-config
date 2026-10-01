// check —— 判断目标机是否可达（Go 版，移植自 scripts/target-check.sh）。
//
// 契约（与 bash 版一致）：只根据 ssh 自己的报错判断，不登录中转机、
// 不改 ssh 选项、不重试别的端口。
// 退出码：0 = 可达；1 = 不可达（报错原文已打印 / 已进 JSON）。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type checkResult struct {
	Alias     string   `json:"alias"`
	Reachable bool     `json:"reachable"`
	Info      []string `json:"info,omitempty"`
	SSHError  string   `json:"ssh_error,omitempty"`
	Category  string   `json:"category,omitempty"`
	Advice    string   `json:"advice,omitempty"`
}

func runCheck(args []string) int {
	jsonOut := false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "-h", "--help":
			fmt.Println("用法：arena check [--json]")
			fmt.Println("经 ProxyJump 通道判定目标机可达性。退出码：0 可达 / 1 不可达 / 2 用法错误。")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "check：未知参数 %s\n", a)
			return 2
		}
	}

	s := loadSettings()
	res := checkResult{Alias: s.target}

	out, err := sshRun(s.target, 60, "echo CONNECTED; hostname; whoami")
	if err == nil && strings.Contains(out, "CONNECTED") {
		res.Reachable = true
		res.Info = infoLines(out)
		if jsonOut {
			emitJSON(res)
		} else {
			fmt.Printf("== 通道：%s（ProxyJump 经中转机 22）==\n", s.target)
			mark(cOK, "OK", "可达："+strings.Join(res.Info, " "))
		}
		return 0
	}

	res.SSHError = strings.TrimRight(out, "\n")
	res.Category, res.Advice = classify(out, s.transit)
	if jsonOut {
		emitJSON(res)
	} else {
		fmt.Printf("== 通道：%s（ProxyJump 经中转机 22）==\n", s.target)
		fmt.Println("  ssh 报错原文：")
		for _, l := range strings.Split(res.SSHError, "\n") {
			fmt.Println("        " + l)
		}
		fmt.Println()
		mark(cFail, "FAIL", res.Advice)
	}
	return 1
}

// classify 把 ssh 报错映射成（类别, 该对用户说的话），与 bash 版同一张表。
func classify(stderr, transit string) (string, string) {
	switch {
	case containsAny(stderr, "Connection refused", "Connection closed", "banner exchange", "kex_exchange"):
		return "tunnel", fmt.Sprintf(`连不上 —— 多半是反向隧道断了。
        请用户在内网机器执行：ssh -R 2222:localhost:22 root@%s
        修好后重试即可；不要自己找绕路方案，也不要去中转机上排查。`, transit)
	case containsAny(stderr, "REMOTE HOST IDENTIFICATION HAS CHANGED", "Host key verification failed"):
		return "hostkey", `主机指纹与 known_hosts 不符。**停下来问用户**：可能对方重装，也可能有人在中间。
        绝对不要 ssh-keygen -R，也不要关 StrictHostKeyChecking。`
	case containsAny(stderr, "Permission denied"):
		return "auth", `认证失败 —— 检查私钥是否完整（重新导入 secrets.json），
        以及服务端是否还有对应的 authorized_keys。`
	case containsAny(stderr, "Could not resolve hostname", "No such file or directory"):
		return "config", "ssh 配置或依赖缺失 —— 先运行 arena import 导入 secrets.json。"
	default:
		return "unknown", "未知错误（见上面的原文）。不要自行猜测或绕路，把原文交给用户。"
	}
}

// infoLines 提取成功输出里的 hostname/whoami 行（去掉 CONNECTED 与空行）。
func infoLines(out string) []string {
	var ls []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && l != "CONNECTED" {
			ls = append(ls, l)
		}
	}
	return ls
}

func emitJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Println(string(b))
}
