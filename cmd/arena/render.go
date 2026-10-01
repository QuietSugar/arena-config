package main

import (
	_ "embed" // go:embed 指令要求（即使只嵌入字符串）
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// 模板打进二进制：arena import 无需克隆仓库即可渲染环境（空沙箱场景）。
// 模板用 {{占位符}} 风格，按序做字符串替换（与旧 python 实现同构）。

//go:embed templates/ssh_config.tmpl
var sshConfigTmpl string

//go:embed templates/AGENTS.local.md.tmpl
var agentsLocalTmpl string

// 占位符里不许出现真实主机信息：本文件只写占位符名。
var leftoverRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

type kv struct{ k, v string }

// render 按序替换 {{KEY}}；有剩余占位符即报错，防止半成品配置被使用。
func render(tmpl string, subs []kv) (string, error) {
	out := tmpl
	for _, p := range subs {
		out = strings.ReplaceAll(out, "{{"+p.k+"}}", p.v)
	}
	if left := leftoverRe.FindAllStringSubmatch(out, -1); left != nil {
		set := map[string]bool{}
		for _, m := range left {
			set[m[1]] = true
		}
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "", fmt.Errorf("模板有未替换的占位符：%v", keys)
	}
	return out, nil
}
