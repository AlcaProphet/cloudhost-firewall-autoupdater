package notifier

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"sync/atomic"

	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// 使用真实发送出口；只替换本地拨号、测试 CA 与时限。
func smtpModeSend(cfg EmailConfig, security string, roots *x509.CertPool, budget time.Duration, localAddress ...string) error {
	cfg.Security = security
	options := &smtpTransport{roots: roots, deadline: budget}
	if len(localAddress) > 0 {
		options.dial = func(network, _ string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout(network, localAddress[0], timeout)
		}
	}
	return (&EmailNotifier{cfg: cfg, transport: options}).send("模式测试", "本地测试正文")
}

type smtpModeServer struct {
	mu    sync.Mutex
	trace []string
	done  chan struct{}
	ln    net.Listener
}

func (s *smtpModeServer) record(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trace = append(s.trace, value)
}
func (s *smtpModeServer) commands() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.trace, "|")
}

func smtpModeCertificate(t *testing.T, wrongName bool) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "local probe"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost", "smtp.mode.invalid"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	if wrongName {
		leaf.DNSNames = []string{"wrong.invalid"}
		leaf.IPAddresses = nil
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, parsed, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: key}, roots
}

func smtpModeServe(t *testing.T, kind string, cert tls.Certificate) (EmailConfig, *smtpModeServer) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &smtpModeServer{done: make(chan struct{}), ln: ln}
	go func() {
		defer close(s.done)
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() {
			if err := raw.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		}()
		if err = raw.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return
		}
		var conn net.Conn = raw
		tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
		if kind == "silent_handshake" {
			if _, err := io.Copy(io.Discard, raw); err != nil {
				t.Logf("静默服务读取结束: %T", err)
			}
			return
		}
		if kind == "implicit" || kind == "silent_greeting" || kind == "delayed_tls" {
			if kind == "delayed_tls" {
				time.Sleep(200 * time.Millisecond)
			}
			encrypted := tls.Server(raw, tlsConfig)
			if err = encrypted.Handshake(); err != nil {
				return
			}
			conn = encrypted
			s.record("TLS")
			if kind == "silent_greeting" || kind == "delayed_tls" {
				if _, err := io.Copy(io.Discard, conn); err != nil {
					t.Logf("静默服务读取结束: %T", err)
				}
				return
			}
		}
		send := func(value string) bool { _, err := io.WriteString(conn, value+"\r\n"); return err == nil }
		if !send("220 local ready") {
			return
		}
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			name, _, _ := strings.Cut(line, " ")
			s.record(name)
			switch name {
			case "EHLO", "HELO":
				if kind == "hello_failure" {
					if !send("550 secret-provider-message") {
						return
					}
					continue
				}
				if kind == "helo_fallback" && name == "EHLO" {
					if !send("500 no ehlo") {
						return
					}
					continue
				}
				if kind == "implicit" || ((kind == "starttls" || kind == "starttls_reject" || kind == "starttls_broken") && conn == raw) {
					if !send("250-local\r\n250-STARTTLS\r\n250 AUTH PLAIN") {
						return
					}
				} else if !send("250-local\r\n250 AUTH PLAIN") {
					return
				}
			case "STARTTLS":
				if kind == "starttls_reject" {
					send("454 secret-provider-message")
					return
				}
				if kind == "starttls_broken" {
					send("220 upgrade")
					send("not-a-tls-record secret-provider-message")
					return
				}
				if !send("220 upgrade") {
					return
				}
				encrypted := tls.Server(raw, tlsConfig)
				if err = encrypted.Handshake(); err != nil {
					return
				}
				conn = encrypted
				reader = bufio.NewReader(conn)
				s.record("TLS")
			case "AUTH":
				if !send("235 accepted") {
					return
				}
			case "MAIL", "RCPT":
				if !send("250 accepted") {
					return
				}
			case "DATA":
				if !send("354 continue") {
					return
				}
				for {
					line, err = reader.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
				}
				if !send("250 accepted") {
					return
				}
			case "QUIT":
				send("221 bye")
				return
			default:
				return
			}
		}
	}()
	t.Cleanup(func() {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			t.Error("server cleanup exceeded budget")
		}
	})
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return EmailConfig{Host: host, Port: port, User: "probe-user", Pass: "probe-password", From: "from@example.test", To: "to@example.test"}, s
}

func TestSMTPSecurityProtocols(t *testing.T) {
	cert, roots := smtpModeCertificate(t, false)
	for _, tc := range []struct{ name, mode, server, want string }{
		{"implicit", "implicit_tls", "implicit", "TLS|EHLO|AUTH|MAIL|RCPT|DATA|QUIT"},
		{"mandatory", "starttls", "starttls", "EHLO|STARTTLS|TLS|EHLO|AUTH|MAIL|RCPT|DATA|QUIT"},
		{"auto-upgrade", "auto_starttls", "starttls", "EHLO|STARTTLS|TLS|EHLO|AUTH|MAIL|RCPT|DATA|QUIT"},
		{"auto-plain", "auto_starttls", "plain", "EHLO|AUTH|MAIL|RCPT|DATA|QUIT"},
		{"auto-helo-fallback", "auto_starttls", "helo_fallback", "EHLO|HELO|AUTH|MAIL|RCPT|DATA|QUIT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, s := smtpModeServe(t, tc.server, cert)
			address := net.JoinHostPort(cfg.Host, cfg.Port)
			if tc.server == "implicit" || tc.server == "starttls" {
				cfg.Host = "smtp.mode.invalid"
			}
			if err := smtpModeSend(cfg, tc.mode, roots, time.Second, address); err != nil {
				t.Fatal(err)
			}
			if got := s.commands(); got != tc.want {
				t.Fatalf("commands=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestSMTPSecurityMandatoryRejects(t *testing.T) {
	cert, roots := smtpModeCertificate(t, false)
	for _, kind := range []string{"plain", "hello_failure", "helo_fallback", "starttls_reject", "starttls_broken"} {
		t.Run(kind, func(t *testing.T) {
			cfg, s := smtpModeServe(t, kind, cert)
			err := smtpModeSend(cfg, "starttls", roots, time.Second)
			if err == nil {
				t.Fatal("mandatory mode accepted an unencrypted session")
			}
			got := s.commands()
			if strings.Contains(got, "AUTH") || strings.Contains(got, "MAIL") || strings.Contains(got, "DATA") {
				t.Fatalf("sent protected commands after rejection: %s", got)
			}
			if strings.Contains(err.Error(), "secret-provider-message") || errors.Unwrap(err) != nil {
				t.Fatal("raw provider diagnostic escaped")
			}
			if kind == "hello_failure" && !strings.Contains(err.Error(), "SMTP EHLO/HELO 失败: SMTP 响应码 550") {
				t.Fatalf("wrong hello diagnosis: %s", err)
			}
		})
	}
}

func TestSMTPSecurityCertificates(t *testing.T) {
	for _, kind := range []string{"untrusted", "hostname"} {
		t.Run(kind, func(t *testing.T) {
			cert, roots := smtpModeCertificate(t, kind == "hostname")
			if kind == "untrusted" {
				roots = x509.NewCertPool()
			}
			cfg, s := smtpModeServe(t, "implicit", cert)
			err := smtpModeSend(cfg, "implicit_tls", roots, time.Second)
			if err == nil || !strings.Contains(err.Error(), "TLS 验证或握手异常") {
				t.Fatalf("certificate not safely rejected: %v", err)
			}
			if errors.Unwrap(err) != nil || strings.Contains(err.Error(), "wrong.invalid") {
				t.Fatal("TLS 诊断泄露底层原因")
			}
			if strings.Contains(s.commands(), "AUTH") {
				t.Fatal("auth after invalid certificate")
			}
		})
	}
}

func TestSMTPSecurityDeadlines(t *testing.T) {
	cert, roots := smtpModeCertificate(t, false)
	for _, kind := range []string{"silent_handshake", "silent_greeting", "delayed_tls"} {
		t.Run(kind, func(t *testing.T) {
			cfg, _ := smtpModeServe(t, kind, cert)
			cfg.Security = config.SMTPSecurityImplicitTLS
			var observed *smtpDeadlineConn
			n := &EmailNotifier{cfg: cfg, transport: &smtpTransport{roots: roots, deadline: 300 * time.Millisecond, dial: func(network, address string, timeout time.Duration) (net.Conn, error) {
				raw, err := net.DialTimeout(network, address, timeout)
				if err != nil {
					return nil, err
				}
				observed = &smtpDeadlineConn{Conn: raw}
				return observed, nil
			}}}
			started := time.Now()
			err := n.send("测试", "本地正文")
			elapsed := time.Since(started)
			if err == nil || !strings.Contains(err.Error(), "会话超时") {
				t.Fatalf("wrong timeout: %v", err)
			}
			if observed == nil || observed.deadlines.Load() != 1 {
				t.Fatal("会话 deadline 未设置一次或被重置")
			}
			if elapsed > time.Second {
				t.Fatalf("session budget reset or unbounded: %s", elapsed)
			}
		})
	}
}

func TestSMTPSecurityInvalidMode(t *testing.T) {
	for _, mode := range []string{"", "unknown-secret"} {
		cfg := EmailConfig{Host: "invalid.example", Port: "invalid", Security: mode}
		dialed := false
		n := &EmailNotifier{cfg: cfg, transport: &smtpTransport{dial: func(string, string, time.Duration) (net.Conn, error) {
			dialed = true
			return nil, errors.New("dial should not run")
		}}}
		err := n.send("subject", "body")
		if dialed || err == nil || err.Error() != "SMTP 安全模式无效: 会话或安全策略异常" {
			t.Fatalf("mode was not rejected before dial: %v", err)
		}
	}
}

func TestSMTPSecurityRemotePlainAuthRejected(t *testing.T) {
	cert, roots := smtpModeCertificate(t, false)
	cfg, server := smtpModeServe(t, "plain", cert)
	address := net.JoinHostPort(cfg.Host, cfg.Port)
	cfg.Host = "smtp.mode.invalid"
	err := smtpModeSend(cfg, config.SMTPSecurityAutoSTARTTLS, roots, time.Second, address)
	if err == nil || !strings.Contains(err.Error(), "SMTP 认证失败") {
		t.Fatalf("非本地明文认证未拒绝: %v", err)
	}
	if strings.Contains(server.commands(), "AUTH") || strings.Contains(server.commands(), "MAIL") {
		t.Fatal("明文下发认证或信封")
	}
}

// 计数接缝保留真实底层连接；隐式 TLS 仍直接传递 *tls.Conn。
type smtpDeadlineConn struct {
	net.Conn
	deadlines atomic.Int32
}

func (c *smtpDeadlineConn) SetDeadline(deadline time.Time) error {
	c.deadlines.Add(1)
	return c.Conn.SetDeadline(deadline)
}

func TestSMTPSecurityAutomaticEvents(t *testing.T) {
	cert, roots := smtpModeCertificate(t, false)
	for _, mode := range []string{config.SMTPSecurityImplicitTLS, config.SMTPSecuritySTARTTLS, config.SMTPSecurityAutoSTARTTLS} {
		for _, event := range []EventType{EventDNSFailed, EventSyncError, EventOperationalUnhealthy} {
			t.Run(mode+"/"+string(event), func(t *testing.T) {
				kind := "starttls"
				if mode == config.SMTPSecurityImplicitTLS {
					kind = "implicit"
				}
				cfg, server := smtpModeServe(t, kind, cert)
				address := net.JoinHostPort(cfg.Host, cfg.Port)
				cfg.Host, cfg.Security = "smtp.mode.invalid", mode
				notifier := NewEmailNotifier(cfg)
				notifier.transport = &smtpTransport{roots: roots, deadline: time.Second, dial: func(network, _ string, timeout time.Duration) (net.Conn, error) {
					return net.DialTimeout(network, address, timeout)
				}}
				if err := notifier.OnEvent(Event{Type: event, Timestamp: time.Now()}); err != nil {
					t.Fatal(err)
				}
				want := "TLS|EHLO|AUTH|MAIL|RCPT|DATA|QUIT"
				if mode != config.SMTPSecurityImplicitTLS {
					want = "EHLO|STARTTLS|TLS|EHLO|AUTH|MAIL|RCPT|DATA|QUIT"
				}
				if server.commands() != want {
					t.Fatalf("自动邮件未使用指定模式: %s", server.commands())
				}
			})
		}
	}
}
