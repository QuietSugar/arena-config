package secrets

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// 现场生成密钥对，避免把任何真实密钥固化进仓库。
func genPair(t *testing.T) (privPEM, pubLine []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "arena-test")
	if err != nil {
		t.Fatal(err)
	}
	privPEM = pem.EncodeToMemory(block)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub
	pubLine = ssh.MarshalAuthorizedKey(signer.PublicKey())
	return privPEM, pubLine
}

func TestCheckPairOK(t *testing.T) {
	priv, pub := genPair(t)
	if err := CheckPair("id_ed25519", priv, pub); err != nil {
		t.Fatalf("配对应通过：%v", err)
	}
}

func TestCheckPairMismatch(t *testing.T) {
	priv, _ := genPair(t)
	_, pub2 := genPair(t)
	if err := CheckPair("id_ed25519", priv, pub2); err == nil {
		t.Fatal("不配对应报错")
	}
	// 非法私钥
	if err := CheckPair("id_ed25519", []byte("not-a-key"), pub2); err == nil {
		t.Fatal("非法私钥应报错")
	}
	// 非法公钥行
	if err := CheckPair("id_ed25519", priv, []byte("garbage")); err == nil {
		t.Fatal("非法公钥行应报错")
	}
}

func TestDecodeKeysEndToEnd(t *testing.T) {
	priv, pub := genPair(t)
	s := &Secrets{SSHKeys: []SSHKey{
		{Name: "id_ed25519", Kind: "private", Mode: "600", DataB64: base64.StdEncoding.EncodeToString(priv)},
		{Name: "id_ed25519.pub", Kind: "public", Mode: "644", DataB64: base64.StdEncoding.EncodeToString(pub)},
	}}
	keys, err := s.DecodeKeys()
	if err != nil {
		t.Fatalf("DecodeKeys 应通过：%v", err)
	}
	if len(keys) != 2 || keys[0].Mode != 0o600 || keys[1].Mode != 0o644 {
		t.Errorf("解码结果不对：%+v", keys)
	}
	// 配对失败（调换 pub）应整体失败
	s.SSHKeys[1].DataB64 = base64.StdEncoding.EncodeToString([]byte("ssh-ed25519 QVJDREVW fake"))
	if _, err := s.DecodeKeys(); err == nil || !strings.Contains(err.Error(), "不配对") {
		t.Errorf("调换公钥应报不配对：%v", err)
	}
	// 名称穿越应被拒
	s.SSHKeys[1].DataB64 = base64.StdEncoding.EncodeToString(pub)
	s.SSHKeys[1].Name = "../evil"
	if _, err := s.DecodeKeys(); err == nil {
		t.Error("路径穿越名称应被拒")
	}
}
