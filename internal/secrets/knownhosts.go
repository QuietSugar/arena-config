package secrets

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// Entry 是一行 known_hosts 的（主机名, 密钥类型, 密钥体）。
type Entry struct {
	Name    string
	KeyType string
	KeyB64  string
}

// BuildEntries 把 host_keys 按三个固定别名映射成 known_hosts 行名，
// 并校验 fingerprint（若提供）。lookup 规则与原 arena-import.sh 一致：
//   - target 用 host_key_alias；未给则 hostname（22 端口）或 [hostname]:port
//   - transit 用 host
//   - github 用 [ssh_host]:port
func (s *Secrets) BuildEntries() ([]Entry, error) {
	var out []Entry
	seen := map[string]bool{}
	for _, hk := range s.HostKeys {
		var name string
		switch hk.Alias {
		case s.Hosts.Target.Alias:
			name = lookupTargetName(s.Hosts.Target)
		case s.Hosts.Transit.Alias:
			name = s.Hosts.Transit.Host
		case s.Hosts.Github.Alias:
			name = bracketName(s.Hosts.Github.SSHHost, s.Hosts.Github.Port)
		default:
			continue
		}
		if err := checkFingerprint(hk); err != nil {
			return nil, err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Entry{Name: name, KeyType: hk.KeyType, KeyB64: hk.KeyB64})
	}
	return out, nil
}

func lookupTargetName(t TargetHost) string {
	if t.HostKeyAlias != "" {
		return t.HostKeyAlias
	}
	return bracketName(t.Hostname, t.Port)
}

func bracketName(host string, port int) string {
	if port == 22 {
		return host
	}
	return fmt.Sprintf("[%s]:%d", host, port)
}

// FingerprintOf 计算 key_b64 的 SHA256 指纹（与 ssh-keygen -lf 输出的 SHA256: 形态一致）。
func FingerprintOf(keyB64 string) (string, error) {
	blob, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return "", fmt.Errorf("主机公钥 base64 解码失败：%w", err)
	}
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

func checkFingerprint(hk HostKey) error {
	if hk.Fingerprint == "" {
		return nil
	}
	fp, err := FingerprintOf(hk.KeyB64)
	if err != nil {
		return err
	}
	if hk.Fingerprint != fp {
		return fmt.Errorf("%s 的 key_b64 与 fingerprint 不符 —— JSON 内部不自洽", hk.Alias)
	}
	return nil
}

// RandomSalt 生成 20 字节随机盐（OPENSSH known_hosts 哈希的盐长）。
func RandomSalt() []byte {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand 不可恢复
	}
	return b
}

// HashName 把主机名哈希成 OPENSSH 的 |1|salt|mac 形态：
// mac = HMAC-SHA1(key=salt, message=hostname)，salt 为 20 字节随机值，
// 两段均按标准 base64（带 padding）编码 —— 与 ssh-keygen -H 逐字节一致。
func HashName(name string, salt []byte) string {
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(name))
	return "|1|" + base64.StdEncoding.EncodeToString(salt) + "|" +
		base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// Render 生成 known_hosts 文件内容（主机名全部哈希）。
func Render(entries []Entry) string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(HashName(e.Name, RandomSalt()))
		sb.WriteString(" " + e.KeyType + " " + e.KeyB64 + "\n")
	}
	return sb.String()
}
