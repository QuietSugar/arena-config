// arena —— arena-config 的单二进制入口。
//
// 设计取向（本工具的主要使用者是 AI agent）：
//   - 每个子命令支持 --json 结构化输出（人类可读输出为默认）
//   - 非交互：任何命令不等待输入，可安全被脚本/agent 调用
//   - 退出码稳定：0 = 成功；1 = 业务失败（连不上/扫到问题）；2 = 用法错误
//   - 报错面向"转述给用户"设计：原始报错原样内嵌
//
// 用法：
//
//	arena --version
//	arena --help
//	arena <子命令> [--json] [子命令参数]
//
// 子命令（陆续迁移中）：
//
//	probe     探明当前环境的出网能力（GitHub 22/443、HTTPS、TLS 签发者）
//
// 版本由构建时 ldflags 注入：-X main.version={{ .Version }}
package main

import (
	"fmt"
	"os"
)

var version = "dev"

type subcommand struct {
	name  string
	blurb string
	run   func(args []string) int
}

var commands = []subcommand{
	{"probe", "探明当前环境的出网能力（GitHub 22/443、HTTPS、TLS 证书签发者），不依赖任何私钥", runProbe},
	{"check", "连通性判定：如实转述 ssh 报错并给出处置建议，支持 --json", runCheck},
	{"import", "把 secrets.json 变成可用环境（密钥/ssh config/known_hosts/派生配置/钩子）", runImport},
	{"bootstrap", "环境自检与自动修复（git 配置/权限/自更新/工具/连通），支持 --json", runBootstrap},
	{"scan", "泄露审计：工作区 + 全历史 + 不可达对象 + 文件名 + 元数据，支持 --json 与 --remote", runScan},
	{"sync", "代码同步：pull/push/diff/status/run/ssh（rsync over ssh，远端路径可省略）", runSync},
}

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

func dispatch(args []string) int {
	if len(args) == 0 {
		usage(os.Stdout)
		return 2
	}
	switch args[0] {
	case "-h", "--help":
		usage(os.Stdout)
		return 0
	case "--version", "-version":
		fmt.Printf("arena %s\n", version)
		return 0
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(args[1:])
		}
	}
	fmt.Fprintf(os.Stderr, "未知子命令：%s\n\n", args[0])
	usage(os.Stderr)
	return 2
}

func usage(w *os.File) {
	fmt.Fprintln(w, "arena —— 在沙箱里安全操作 NAT 后目标机的单二进制工具")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "用法：arena <子命令> [--json] [参数]   （退出码：0 成功 / 1 业务失败 / 2 用法错误）")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "子命令：")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.blurb)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "通用选项：--json 输出结构化结果（供 agent/脚本消费）；--help 查看本说明")
}
