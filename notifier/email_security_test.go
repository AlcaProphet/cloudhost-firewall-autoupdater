package notifier

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"
)

const p319Password = "p319-password-canary"
const p319User = "p319-user-canary"
const p319Body = "p319-body-canary"

func p319SMTP(t *testing.T, stage string) (string, string) {
	t.Helper()
	var tlsConfig *tls.Config
	if stage == "tls_handshake" {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: p319Password}, DNSNames: []string{p319Password}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return
		}
		r := bufio.NewReader(conn)
		send := func(s string) bool { _, err := io.WriteString(conn, s+"\r\n"); return err == nil }
		token := base64.StdEncoding.EncodeToString([]byte("\x00" + p319User + "\x00" + p319Password))
		leak := "echo=" + p319Password + " " + p319User + " " + p319Body + " " + token
		if stage == "greeting" {
			send("554 " + leak)
			return
		}
		if stage == "malformed_greeting" {
			send("invalid " + leak)
			return
		}
		if stage == "silent" {
			_, _ = r.ReadString('\n')
			return
		}
		if !send("220 fake ready") {
			return
		}
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				if stage == "hello" {
					if !send("550 " + leak) {
						return
					}
					continue
				}
				if stage == "starttls" || stage == "tls_handshake" {
					if !send("250-fake\r\n250 STARTTLS") {
						return
					}
					continue
				}
				if !send("250-fake\r\n250 AUTH PLAIN") {
					return
				}
			case strings.HasPrefix(line, "STARTTLS"):
				if stage == "tls_handshake" {
					if !send("220 upgrade") {
						return
					}
					server := tls.Server(conn, tlsConfig)
					_ = server.Handshake()
					return
				}
				if !send("454 " + leak) {
					return
				}
			case strings.HasPrefix(line, "AUTH"):
				if !strings.Contains(line, token) {
					return
				}
				if stage == "auth" {
					if !send("535 5.7.8 " + leak) {
						return
					}
					continue
				}
				if stage == "auth_multiline" {
					if !send("535-5.7.8 " + leak + "\r\n535 5.7.8 " + leak) {
						return
					}
					continue
				}
				if stage == "malformed_auth" {
					if !send("invalid " + leak) {
						return
					}
					continue
				}
				if !send("235 auth ok") {
					return
				}
			case strings.HasPrefix(line, "MAIL FROM"):
				if stage == "mail" {
					if !send("550 " + leak) {
						return
					}
					continue
				}
				if !send("250 ok") {
					return
				}
			case strings.HasPrefix(line, "RCPT TO"):
				if stage == "rcpt" {
					if !send("550 " + leak) {
						return
					}
					continue
				}
				if !send("250 ok") {
					return
				}
			case strings.HasPrefix(line, "DATA"):
				if stage == "data" {
					if !send("554 " + leak) {
						return
					}
					continue
				}
				if !send("354 continue") {
					return
				}
				for {
					dl, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if dl == ".\r\n" {
						break
					}
				}
				if stage == "data_end" {
					if !send("554 " + leak) {
						return
					}
					continue
				}
				if !send("250 queued") {
					return
				}
			case strings.HasPrefix(line, "QUIT"):
				if stage == "quit" {
					send("554 " + leak)
					return
				}
				send("221 bye")
				return
			default:
				if !send("501 aborted") {
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("SMTP fixture did not stop")
		}
	})
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
func p319AssertSafe(t *testing.T, got string) {
	t.Helper()
	for _, secret := range []string{p319Password, p319User, p319Body, base64.StdEncoding.EncodeToString([]byte("\x00" + p319User + "\x00" + p319Password)), "echo="} {
		if strings.Contains(got, secret) {
			t.Error("SMTP error leaked synthetic server content")
		}
	}
	if len(got) > 1024 {
		t.Error("unbounded diagnostic")
	}
}

func TestP319Session(t *testing.T) {
	for _, tc := range []struct{ stage, want string }{
		{"greeting", "554"}, {"malformed_greeting", "SMTP 响应格式异常"}, {"hello", "550"}, {"starttls", "454"},
		{"auth", "535"}, {"auth_multiline", "535"}, {"malformed_auth", "SMTP 响应格式异常"}, {"mail", "550"},
		{"rcpt", "550"}, {"data", "554"}, {"data_end", "554"}, {"quit", "554"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			host, port := p319SMTP(t, tc.stage)
			err := SendTestEmail(EmailConfig{Host: host, Port: port, User: p319User, Pass: p319Password, From: "f@example.com", To: "t@example.com"}, "subject", p319Body)
			if err == nil {
				t.Fatal("expected failure")
			}
			p319AssertSafe(t, err.Error())
			if !strings.Contains(err.Error(), tc.want) {
				t.Error("safe diagnosis lost code/category")
			}
			var raw *textproto.Error
			if errors.As(err, &raw) {
				t.Error("raw SMTP cause exposed")
			}
			var protocol textproto.ProtocolError
			if errors.As(err, &protocol) {
				t.Error("raw protocol cause exposed")
			}
			if errors.Unwrap(err) != nil {
				t.Error("raw cause reachable by Unwrap")
			}
		})
	}
}
func TestP319BusLog(t *testing.T) {
	host, port := p319SMTP(t, "auth")
	prev := slog.Default()
	oldWriter, oldFlags := log.Writer(), log.Flags()
	events := make(chan string, 1)
	slog.SetDefault(slog.New(p319Handler{events: events}))
	defer func() { slog.SetDefault(prev); log.SetOutput(oldWriter); log.SetFlags(oldFlags) }()
	bus := NewEventBus()
	bus.Subscribe(EventSyncError, NewEmailNotifier(EmailConfig{Host: host, Port: port, User: p319User, Pass: p319Password, From: "f@example.com", To: "t@example.com"}))
	bus.Publish(Event{Type: EventSyncError, Timestamp: time.Now()})
	select {
	case got := <-events:
		p319AssertSafe(t, got)
		if !strings.Contains(got, "535") {
			t.Error("missing SMTP code")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing bus WARN")
	}
}

type p319Handler struct{ events chan string }

func (h p319Handler) Enabled(context.Context, slog.Level) bool { return true }
func (h p319Handler) Handle(ctx context.Context, r slog.Record) error {
	var b bytes.Buffer
	if err := slog.NewTextHandler(&b, nil).Handle(ctx, r); err != nil {
		return err
	}
	if r.Message == "事件处理失败" {
		h.events <- b.String()
	}
	return nil
}
func (h p319Handler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h p319Handler) WithGroup(string) slog.Handler      { return h }

type p319UnknownError struct{}

func (p319UnknownError) Error() string { panic("unknown Error method must not be called") }
func TestP319Classification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"会话超时", &net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}, "会话超时"},
		{"网络连接异常", &net.DNSError{Name: p319Password, Err: p319Body}, "网络连接异常"},
		{"TLS", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "TLS 验证或握手异常"},
		{"TLS_record", tls.RecordHeaderError{Msg: p319Password}, "TLS 验证或握手异常"},
		{"EOF", io.EOF, "连接已关闭"},
		{"wrapped_closed", &net.OpError{Op: "read", Err: net.ErrClosed}, "连接已关闭"},
		{"TLS_hostname", x509.HostnameError{}, "TLS 验证或握手异常"},
		{"TLS_invalid", x509.CertificateInvalidError{}, "TLS 验证或握手异常"},
		{"TLS_authority", x509.UnknownAuthorityError{}, "TLS 验证或握手异常"},
		{"unexpected_success_code", &textproto.Error{Code: 299, Msg: p319Password}, "299"},
		{"code_below_range", &textproto.Error{Code: 199, Msg: p319Password}, "SMTP 响应格式异常"},
		{"SMTP 响应格式异常", textproto.ProtocolError(p319Password), "SMTP 响应格式异常"},
		{"base64", base64.CorruptInputError(9), "SMTP 响应格式异常"},
		{"unknown", p319UnknownError{}, "会话或安全策略异常"},
		{"wrapped", fmt.Errorf("outer %s: %w", p319Password, &textproto.Error{Code: 535, Msg: p319Password}), "535"},
		{"code_out_of_range", &textproto.Error{Code: 999, Msg: p319Password}, "SMTP 响应格式异常"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := safeSMTPError("SMTP 认证失败", tc.err)
			p319AssertSafe(t, got.Error())
			if !strings.Contains(got.Error(), tc.want) {
				t.Error("wrong safe category")
			}
			if errors.Is(got, tc.err) || errors.Unwrap(got) != nil {
				t.Error("source error retained")
			}
		})
	}
}
func TestP319TimeoutStillBounded(t *testing.T) {
	host, port := p319SMTP(t, "silent")
	setSMTPTimeouts(t, 100*time.Millisecond, 80*time.Millisecond)
	start := time.Now()
	err := SendTestEmail(EmailConfig{Host: host, Port: port}, "s", "b")
	if err == nil || !strings.Contains(err.Error(), "会话超时") {
		t.Error("missing timeout classification")
	}
	if time.Since(start) > time.Second {
		t.Error("SMTP deadline lost")
	}
}

func TestP319LargeDiagnostic(t *testing.T) {
	err := safeSMTPError("SMTP 认证失败", &textproto.Error{Code: 535, Msg: strings.Repeat(p319Password, 100000)})
	if err.Error() != "SMTP 认证失败: SMTP 响应码 535" {
		t.Fatal("unsafe or unbounded error")
	}
	p319AssertSafe(t, err.Error())
}

func TestP319LocalTLS(t *testing.T) {
	host, port := p319SMTP(t, "tls_handshake")
	err := SendTestEmail(EmailConfig{Host: host, Port: port, User: p319User, Pass: p319Password}, "s", p319Body)
	if err == nil || err.Error() != "STARTTLS 失败: TLS 验证或握手异常" {
		t.Fatal("missing safe TLS failure")
	}
	p319AssertSafe(t, err.Error())
	if errors.Unwrap(err) != nil {
		t.Fatal("TLS certificate error retained")
	}
}

func TestP319DialFailure(t *testing.T) {
	err := SendTestEmail(EmailConfig{Host: p319Password, Port: "65536"}, "s", p319Body)
	if err == nil || err.Error() != "连接 SMTP 服务器失败: 网络连接异常" {
		t.Fatal("dial failure was not safely classified")
	}
	p319AssertSafe(t, err.Error())
	if errors.Unwrap(err) != nil {
		t.Fatal("dial cause retained")
	}
}
func TestP319AuthPolicy(t *testing.T) {
	auth := smtp.PlainAuth("", p319User, p319Password, "smtp.invalid")
	_, payload, err := auth.Start(&smtp.ServerInfo{Name: "smtp.invalid", TLS: false})
	if err == nil || len(payload) != 0 {
		t.Fatal("unencrypted non-local auth must reject credentials")
	}
	safe := safeSMTPError("SMTP 认证失败", err)
	if safe.Error() != "SMTP 认证失败: 会话或安全策略异常" {
		t.Fatal("untyped auth policy error must use fixed safe fallback")
	}
	p319AssertSafe(t, safe.Error())
	if errors.Unwrap(safe) != nil {
		t.Fatal("auth policy cause retained")
	}
}
