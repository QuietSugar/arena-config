package secrets

import (
	"encoding/base64"
	"strings"
	"testing"
)

func validJSON(t *testing.T) string {
	t.Helper()
	return `{
	  "schema": "arena-secrets/1",
	  "ssh_keys": [
	    {"name": "id_ed25519", "kind": "private", "mode": "600", "data_b64": "LS0tLS1CRUdJTiBPUEVOU1NIIFBSSVZBVEUgS0VZLS0tLS0="},
	    {"name": "id_ed25519.pub", "kind": "public", "mode": "644", "data_b64": "c3NoLWVkMjU1MTkgQUFBQQ=="}
	  ],
	  "hosts": {
	    "target":  {"alias": "vm", "hostname": "127.0.0.1", "port": 2222, "user": "dev", "key": "id_ed25519"},
	    "transit": {"alias": "transit", "host": "203.0.113.10", "user": "root", "key": "id_ed25519"},
	    "github":  {"alias": "github", "ssh_host": "ssh.github.com", "port": 443, "user": "git", "key": "id_ed25519"}
	  },
	  "host_keys": [
	    {"alias": "vm", "key_type": "ssh-ed25519", "key_b64": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="}
	  ],
	  "tooling": {"secret_patterns": ["x"], "default_remote_root": "/tmp/x"}
	}`
}

func TestParseValid(t *testing.T) {
	s, err := Parse([]byte(validJSON(t)))
	if err != nil {
		t.Fatalf("合法 JSON 应通过：%v", err)
	}
	if s.Hosts.Target.Alias != "vm" || s.Hosts.Target.Port != 2222 {
		t.Errorf("target 解析不对：%+v", s.Hosts.Target)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"schema 缺失":      strings.Replace(validJSON(t), `"schema": "arena-secrets/1"`, `"schema": ""`, 1),
		"schema 版本旧":     strings.Replace(validJSON(t), `"arena-secrets/1"`, `"arena-secrets/0"`, 1),
		"无 ssh_keys":     strings.Replace(validJSON(t), `"ssh_keys"`, `"nope_keys"`, 1),
		"target 无 alias": strings.Replace(validJSON(t), `"alias": "vm"`, `"alias": ""`, 1),
		"端口为 0":          strings.Replace(validJSON(t), `"port": 2222`, `"port": 0`, 1),
	}
	for name, js := range cases {
		if _, err := Parse([]byte(js)); err == nil {
			t.Errorf("%s：应报错但通过", name)
		}
	}
	if _, err := Parse([]byte("{broken")); err == nil {
		t.Error("坏 JSON 应报错")
	}
}

func TestValidateKeyName(t *testing.T) {
	for _, bad := range []string{"../x", "a/b", ".hidden", ""} {
		if err := ValidateKeyName(bad); err == nil {
			t.Errorf("%q 应被拒绝", bad)
		}
	}
	for _, good := range []string{"id_ed25519", "github_deploy", "key-with-dash"} {
		if err := ValidateKeyName(good); err != nil {
			t.Errorf("%q 应通过：%v", good, err)
		}
	}
}

func TestBuildEntriesLookupRules(t *testing.T) {
	s, err := Parse([]byte(validJSON(t)))
	if err != nil {
		t.Fatal(err)
	}
	// target 无 host_key_alias → [hostname]:port
	entries, err := s.BuildEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "[127.0.0.1]:2222" {
		t.Errorf("target 行名不对：%+v", entries)
	}
	// host_key_alias 优先
	s.Hosts.Target.HostKeyAlias = "my-alias"
	entries, _ = s.BuildEntries()
	if entries[0].Name != "my-alias" {
		t.Errorf("host_key_alias 应优先：%+v", entries[0])
	}
	// github → [ssh_host]:port；transit → host
	s.Hosts.Target.HostKeyAlias = ""
	s.HostKeys = append(s.HostKeys,
		HostKey{Alias: "transit", KeyType: "ssh-ed25519", KeyB64: "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="},
		HostKey{Alias: "github", KeyType: "ssh-ed25519", KeyB64: "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="})
	entries, _ = s.BuildEntries()
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	for _, want := range []string{"[127.0.0.1]:2222", "203.0.113.10", "[ssh.github.com]:443"} {
		if !names[want] {
			t.Errorf("缺行名 %q，实际：%v", want, names)
		}
	}
}

func TestFingerprintMismatch(t *testing.T) {
	js := strings.Replace(validJSON(t),
		`"host_keys": [
	    {"alias": "vm", "key_type": "ssh-ed25519", "key_b64": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="}
	  ]`,
		`"host_keys": [
	    {"alias": "vm", "key_type": "ssh-ed25519", "key_b64": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "fingerprint": "SHA256:wrongwrongwrong"}
	  ]`, 1)
	s, err := Parse([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BuildEntries(); err == nil || !strings.Contains(err.Error(), "不自洽") {
		t.Errorf("fingerprint 不符应报不自洽：%v", err)
	}
}

// TestHashNameVector 是 ssh-keygen -H 的实测向量（salt 固定）：
//
//	echo "<host> ssh-ed25519 <blob>" | ssh-keygen -H
//
// 输出首 token 必须为下述值 —— 钉死算法、盐长与 base64 padding。
func TestHashNameVector(t *testing.T) {
	salt, err := base64.StdEncoding.DecodeString("nsOb6QmX5LHSZCdzRPbqyHGH2HA=")
	if err != nil {
		t.Fatal(err)
	}
	got := HashName("example.com", salt)
	want := "|1|nsOb6QmX5LHSZCdzRPbqyHGH2HA=|mr3xdIyGkIDtTDrjYDG+iegdQPk="
	if got != want {
		t.Errorf("HashName 向量不符：\n got %s\nwant %s", got, want)
	}
}
