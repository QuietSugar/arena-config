package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// 输出配色（与 bash 版一致）。
const (
	cOK    = "\033[32m"
	cFail  = "\033[31m"
	cWarn  = "\033[33m"
	cReset = "\033[0m"
)

func mark(color, tag, msg string) {
	fmt.Printf("  %s[%s]%s %s\n", color, tag, cReset, msg)
}

// sshRun 在指定别名上执行一条命令，返回合并输出。超时被杀，按失败处理。
func sshRun(alias string, timeoutSec int, cmd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "ssh", alias, cmd).CombinedOutput()
	return string(b), err
}

type settings struct {
	target  string
	transit string
}

// loadSettings 从 ~/.arena-secrets/env.sh 读取别名与中转机（缺省值与 bash 版一致）。
func loadSettings() settings {
	s := settings{target: "target", transit: "<中转机>"}
	b, err := os.ReadFile(os.Getenv("HOME") + "/.arena-secrets/env.sh")
	if err != nil {
		return s
	}
	m := parseExports(string(b))
	if v := m["ARENA_TARGET_ALIAS"]; v != "" {
		s.target = v
	}
	if v := m["ARENA_TRANSIT_HOST"]; v != "" {
		s.transit = v
	}
	return s
}

// parseExports 解析 env.sh 里的 export KEY='VALUE' 行。
func parseExports(txt string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(txt, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		k, v, found := strings.Cut(line, "=")
		if !found || k == "" || strings.ContainsAny(k, " \t") {
			continue
		}
		m[k] = strings.Trim(strings.TrimSpace(v), `'"`)
	}
	return m
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
