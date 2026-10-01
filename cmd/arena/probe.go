// probe —— 探明当前环境的出网能力（Go 版，移植自 scripts/egress-probe.sh）。
//
// 规则（AGENTS.md）：只探测通用出网能力（GitHub），不探测中转机。
// 退出码：0 = 完成探测（结果本身见输出，探测总能完成）。
package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type probeResult struct {
	ProxyEnv  map[string]string `json:"proxy_env"`
	TCP       map[string]string `json:"tcp"`
	SSH       map[string]string `json:"ssh"`
	HTTPSCode int               `json:"https_github_status"`
	TLSIssuer string            `json:"tls_issuer_github"`
}

func runProbe(args []string) int {
	jsonOut := false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "-h", "--help":
			fmt.Println("用法：arena probe [--json]")
			fmt.Println("探测 GitHub 的 22/443 连通、HTTPS 状态码、TLS 证书签发者。退出码恒 0。")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "probe：未知参数 %s\n", a)
			return 2
		}
	}

	r := probeResult{ProxyEnv: map[string]string{}, TCP: map[string]string{}, SSH: map[string]string{}}
	for _, e := range os.Environ() {
		k, v, _ := strings.Cut(e, "=")
		if strings.EqualFold(k, "https_proxy") || strings.EqualFold(k, "http_proxy") ||
			strings.EqualFold(k, "all_proxy") || strings.EqualFold(k, "no_proxy") {
			r.ProxyEnv[k] = v
		}
	}
	for _, hp := range []string{"github.com:22", "ssh.github.com:443"} {
		host, port, _ := net.SplitHostPort(hp)
		r.TCP[hp] = tcpStatus(host, port)
		r.SSH[hp] = sshBannerStatus(host, port)
	}
	r.HTTPSCode = httpsStatus("https://github.com")
	r.TLSIssuer = tlsIssuer("github.com:443", "github.com")

	if jsonOut {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(string(b))
		return 0
	}

	fmt.Println("== 代理环境变量 ==")
	if len(r.ProxyEnv) == 0 {
		fmt.Println("(无)")
	} else {
		keys := sortedKeys(r.ProxyEnv)
		for _, k := range keys {
			fmt.Printf("%s=%s\n", k, r.ProxyEnv[k])
		}
	}
	fmt.Println("== 原始 TCP ==")
	for _, hp := range []string{"github.com:22", "ssh.github.com:443"} {
		fmt.Printf("%-18s TCP %-8s %s\n", hp, r.TCP[hp], r.SSH[hp])
	}
	fmt.Println("== HTTPS ==")
	fmt.Printf("https://github.com         %d\n", r.HTTPSCode)
	fmt.Println("== TLS 证书签发者（被中间人解密时会显示代理自己的 CA）==")
	if r.TLSIssuer == "" {
		fmt.Println("(TLS 握手失败)")
	} else {
		fmt.Println(r.TLSIssuer)
	}
	return 0
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// tcpStatus 报告原始 TCP 连通性；open / BLOCKED。
func tcpStatus(host, port string) string {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 6*time.Second)
	if err != nil {
		return "BLOCKED"
	}
	conn.Close()
	return "open"
}

// sshBannerStatus 尝试 SSH 握手（空密钥、BatchMode），能收到认证挑战即 "SSH可用"。
// 与 scripts/egress-probe.sh 的 ks() 同语义。
func sshBannerStatus(host, port string) string {
	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityFile=/dev/null",
		"-o", "ConnectTimeout=6",
		"-p", port,
		"probe@"+host, "true")
	out, err := cmd.CombinedOutput()
	s := strings.ToLower(string(out))
	if err == nil || strings.Contains(s, "permission denied") || strings.Contains(s, "authentications that can continue") {
		return "SSH可用"
	}
	return "SSH不通"
}

// httpsStatus GET 一个 URL，返回状态码；失败返回 0。
func httpsStatus(url string) int {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// tlsIssuer 返回对端证书的 Issuer；握手失败返回空串。
func tlsIssuer(addr, serverName string) string {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 8 * time.Second}, "tcp", addr, &tls.Config{ServerName: serverName})
	if err != nil {
		return ""
	}
	defer conn.Close()
	issuer := conn.ConnectionState().PeerCertificates[0].Issuer
	var parts []string
	if issuer.CommonName != "" {
		parts = append(parts, "CN="+issuer.CommonName)
	}
	if len(issuer.Organization) > 0 {
		parts = append(parts, "O="+strings.Join(issuer.Organization, "+"))
	}
	if len(issuer.Country) > 0 {
		parts = append(parts, "C="+issuer.Country[0])
	}
	return strings.Join(parts, ",")
}
