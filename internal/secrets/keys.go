package secrets

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// DecodedKey 是一份已解码、已校验的密钥。
type DecodedKey struct {
	Name string
	Mode uint32 // 权限位（八进制解析）
	Data []byte
}

// DecodeKeys 解码全部密钥并做完整校验（名称合法、私钥可解析、私钥↔公钥配对）。
// 全部通过后才应写盘 —— 失败时不落任何文件。
func (s *Secrets) DecodeKeys() ([]DecodedKey, error) {
	byName := map[string][]byte{}
	for _, k := range s.SSHKeys {
		if err := ValidateKeyName(k.Name); err != nil {
			return nil, err
		}
		b, err := k.Decode()
		if err != nil {
			return nil, err
		}
		byName[k.Name] = b
	}
	// 私钥 ↔ 同名 .pub 配对断言
	for _, k := range s.SSHKeys {
		if k.Kind != "private" {
			continue
		}
		pub, ok := byName[k.Name+".pub"]
		if !ok {
			continue // 未提供同名 .pub，跳过配对检查
		}
		if err := CheckPair(k.Name, byName[k.Name], pub); err != nil {
			return nil, err
		}
	}
	var out []DecodedKey
	for _, k := range s.SSHKeys {
		out = append(out, DecodedKey{Name: k.Name, Mode: parseMode(k.Mode, k.Kind), Data: byName[k.Name]})
	}
	return out, nil
}

// CheckExpectedPubkey 校验私钥文件推导出的公钥 blob 是否与期望值一致（bootstrap 自检用）。
// 期望值为 tooling.expected_pubkey —— 即 secrets.json 里声明的那把公钥的 base64。
func CheckExpectedPubkey(privPath, expectedB64 string) error {
	priv, err := os.ReadFile(privPath)
	if err != nil {
		return fmt.Errorf("读私钥 %s 失败：%w", privPath, err)
	}
	raw, err := ssh.ParseRawPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("%s 不是可解析的私钥：%w", privPath, err)
	}
	s, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		return fmt.Errorf("%s 无法转换为签名器：%w", privPath, err)
	}
	got := base64.StdEncoding.EncodeToString(s.PublicKey().Marshal())
	if got != expectedB64 {
		return fmt.Errorf("私钥与预期公钥不匹配（%s）—— 不要自己生成新密钥；确认导入的是正确的那份 secrets.json", privPath)
	}
	return nil
}

func parseMode(mode, kind string) uint32 {
	if mode == "" {
		if kind == "public" {
			return 0o644
		}
		return 0o600
	}
	var m uint32
	fmt.Sscanf(mode, "%o", &m)
	if m == 0 {
		m = 0o600
	}
	return m
}

// CheckPair 校验私钥可解析，且其推导出的公钥与给定的 .pub 内容一致（类型 + 密钥体）。
func CheckPair(privName string, priv, pub []byte) error {
	raw, err := ssh.ParseRawPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("%s 不是可解析的私钥：%w", privName, err)
	}
	s, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		return fmt.Errorf("%s 无法转换为签名器：%w", privName, err)
	}
	derivedType := s.PublicKey().Type()
	derivedB64 := base64.StdEncoding.EncodeToString(s.PublicKey().Marshal())

	fields := strings.Fields(string(pub))
	if len(fields) < 2 {
		return fmt.Errorf("%s.pub 内容不是合法的公钥行", privName)
	}
	if fields[0] != derivedType || fields[1] != derivedB64 {
		return fmt.Errorf("%s 与 %s.pub 不配对 —— 多半是 JSON 里弄混了", privName, privName)
	}
	return nil
}
