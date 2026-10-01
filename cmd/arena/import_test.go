package main

import (
	"strings"
	"testing"
)

func TestRenderReplacesAll(t *testing.T) {
	tmpl := "Host {{TARGET_ALIAS}}\n    User {{TARGET_USER}}\n"
	out, err := render(tmpl, []kv{{"TARGET_ALIAS", "vm"}, {"TARGET_USER", "dev"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "{{") || !strings.Contains(out, "Host vm") {
		t.Errorf("渲染不对：%q", out)
	}
}

func TestRenderDetectsLeftover(t *testing.T) {
	_, err := render("Host {{TARGET_ALIAS}} {{WS_DOMAIN}}", []kv{{"TARGET_ALIAS", "vm"}})
	if err == nil || !strings.Contains(err.Error(), "WS_DOMAIN") {
		t.Errorf("应报出剩余占位符 WS_DOMAIN：%v", err)
	}
}

func TestEmbeddedTemplatesRenderClean(t *testing.T) {
	subs := []kv{
		{"SSH_DIR", "/home/x/.ssh"}, {"TARGET_ALIAS", "vm"}, {"TARGET_HOSTNAME", "127.0.0.1"},
		{"TARGET_PORT", "2222"}, {"TARGET_USER", "dev"}, {"TARGET_HOST_KEY_ALIAS", "k"},
		{"TARGET_KEY", "id_ed25519"}, {"TRANSIT_ALIAS", "transit"}, {"TRANSIT_HOST", "203.0.113.10"},
		{"TRANSIT_USER", "root"}, {"TRANSIT_KEY", "id_ed25519"}, {"GITHUB_ALIAS", "github"},
		{"GITHUB_SSH_HOST", "ssh.github.com"}, {"GITHUB_PORT", "443"}, {"GITHUB_USER", "git"},
		{"GITHUB_KEY", "github_deploy"},
	}
	if _, err := render(sshConfigTmpl, subs); err != nil {
		t.Errorf("ssh_config.tmpl 渲染失败：%v", err)
	}
}

func TestBashQuote(t *testing.T) {
	if got := bashQuote("vm-lan"); got != "'vm-lan'" {
		t.Errorf("bashQuote = %s", got)
	}
	got := bashQuote("a'b")
	if !strings.Contains(got, `\'`) && !strings.Contains(got, `"`) {
		t.Errorf("含单引号的值应安全转义：%s", got)
	}
}
