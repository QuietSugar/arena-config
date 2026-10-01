package main

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		stderr  string
		wantCat string
		wantSub string
	}{
		{"隧道断", "kex_exchange_identification: Connection closed by remote host", "tunnel", "反向隧道"},
		{"连接拒绝", "channel 0: open failed: connect failed: Connection refused", "tunnel", "反向隧道"},
		{"指纹变更", "REMOTE HOST IDENTIFICATION HAS CHANGED", "hostkey", "停下来问用户"},
		{"认证失败", "Permission denied (publickey)", "auth", "认证失败"},
		{"配置缺失", "Could not resolve hostname my-target", "config", "arena import"},
		{"未知错误", "some weird failure", "unknown", "原文交给用户"},
	}
	for _, c := range cases {
		cat, advice := classify(c.stderr, "203.0.113.10")
		if cat != c.wantCat {
			t.Errorf("%s: category = %q，want %q", c.name, cat, c.wantCat)
		}
		if !strings.Contains(advice, c.wantSub) {
			t.Errorf("%s: 建议里应包含 %q，实际：%s", c.name, c.wantSub, advice)
		}
	}
}

func TestClassifyTransitInterpolated(t *testing.T) {
	_, advice := classify("banner exchange timeout", "203.0.113.10")
	if !strings.Contains(advice, "root@203.0.113.10") {
		t.Errorf("处置建议里应内联中转机地址，实际：%s", advice)
	}
}

func TestParseExports(t *testing.T) {
	txt := "# 注释\n" +
		"export ARENA_TARGET_ALIAS='my-target'\n" +
		"export ARENA_TRANSIT_HOST='203.0.113.10'\n" +
		"不是 export 行=应被忽略\n"
	m := parseExports(txt)
	if m["ARENA_TARGET_ALIAS"] != "my-target" || m["ARENA_TRANSIT_HOST"] != "203.0.113.10" {
		t.Errorf("解析结果不对：%v", m)
	}
	if _, bad := m["不是 export 行"]; bad {
		t.Errorf("非法键不应入库：%v", m)
	}
}

func TestInfoLines(t *testing.T) {
	got := infoLines("CONNECTED\nsandbox-vm\ndev\n\n")
	want := []string{"sandbox-vm", "dev"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("infoLines = %v，want %v", got, want)
	}
}
