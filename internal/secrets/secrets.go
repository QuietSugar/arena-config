// Package secrets —— secrets.json（arena-secrets/1）的解析与校验。
// 仓库与二进制里都不含机密；本包只处理运行时传入的明文 JSON。
package secrets

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Secrets 是 secrets.json 的根结构。wstunnel 字段已随主通道移除，旧 JSON 里的该字段被忽略。
type Secrets struct {
	Schema   string    `json:"schema"`
	SSHKeys  []SSHKey  `json:"ssh_keys"`
	Hosts    Hosts     `json:"hosts"`
	HostKeys []HostKey `json:"host_keys"`
	Tooling  Tooling   `json:"tooling"`
}

type SSHKey struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // private | public
	Mode    string `json:"mode"` // 八进制字符串，如 "600"
	DataB64 string `json:"data_b64"`
}

type Hosts struct {
	Target  TargetHost  `json:"target"`
	Transit TransitHost `json:"transit"`
	Github  GithubHost  `json:"github"`
}

type TargetHost struct {
	Alias        string `json:"alias"`
	Hostname     string `json:"hostname"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	HostKeyAlias string `json:"host_key_alias"`
	LANIP        string `json:"lan_ip"`
	Key          string `json:"key"`
}

type TransitHost struct {
	Alias string `json:"alias"`
	Host  string `json:"host"`
	User  string `json:"user"`
	Key   string `json:"key"`
}

type GithubHost struct {
	Alias    string `json:"alias"`
	SSHHost  string `json:"ssh_host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Key      string `json:"key"`
	CloneURL string `json:"clone_url"`
}

type HostKey struct {
	Alias       string `json:"alias"`
	KeyType     string `json:"key_type"`
	KeyB64      string `json:"key_b64"`
	Fingerprint string `json:"fingerprint"` // 仅校验用；可为空
}

type Tooling struct {
	SecretPatterns    []string `json:"secret_patterns"`
	ExpectedPubkey    string   `json:"expected_pubkey"`
	DefaultRemoteRoot string   `json:"default_remote_root"`
	SyncAlias         string   `json:"sync_alias"`
}

var schemaRe = regexp.MustCompile(`^arena-secrets/\d+$`)

// Parse 解析并校验 secrets.json；返回的 Secrets 可直接用于后续派生。
func Parse(data []byte) (*Secrets, error) {
	var s Secrets
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("JSON 解析失败：%w", err)
	}
	if !schemaRe.MatchString(s.Schema) {
		return nil, fmt.Errorf("schema 字段不合法：%q（应为 arena-secrets/1）", s.Schema)
	}
	if s.Schema != "arena-secrets/1" {
		return nil, fmt.Errorf("schema 主版本不支持：%s（本工具支持 arena-secrets/1）", s.Schema)
	}
	if len(s.SSHKeys) == 0 {
		return nil, fmt.Errorf("缺少必需字段：ssh_keys")
	}
	t := s.Hosts.Target
	for _, kv := range []struct{ name, val string }{
		{"hosts.target.alias", t.Alias}, {"hosts.target.hostname", t.Hostname},
		{"hosts.target.user", t.User}, {"hosts.target.key", t.Key},
		{"hosts.transit.alias", s.Hosts.Transit.Alias}, {"hosts.transit.host", s.Hosts.Transit.Host},
		{"hosts.transit.user", s.Hosts.Transit.User}, {"hosts.transit.key", s.Hosts.Transit.Key},
		{"hosts.github.alias", s.Hosts.Github.Alias}, {"hosts.github.ssh_host", s.Hosts.Github.SSHHost},
		{"hosts.github.user", s.Hosts.Github.User}, {"hosts.github.key", s.Hosts.Github.Key},
	} {
		if kv.val == "" {
			return nil, fmt.Errorf("缺少必需字段：%s", kv.name)
		}
	}
	if t.Port == 0 || s.Hosts.Github.Port == 0 {
		return nil, fmt.Errorf("缺少必需字段：hosts.*.port")
	}
	return &s, nil
}

// Decode 解码 base64 内容（字节精确）。
func (k *SSHKey) Decode() ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(k.DataB64)
	if err != nil {
		return nil, fmt.Errorf("密钥 %s 的 base64 解码失败：%w", k.Name, err)
	}
	return b, nil
}

// ValidateKeyName 拒绝路径穿越：含 "/" 或以 "." 开头的名字不允许。
func ValidateKeyName(name string) error {
	if name == "" || strings.HasPrefix(name, ".") || strings.Contains(name, "/") {
		return fmt.Errorf("密钥名不合法：%s", name)
	}
	return nil
}
