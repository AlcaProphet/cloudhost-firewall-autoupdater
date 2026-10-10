package api

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const p319Password = "p319-password-canary"
const p319User = "p319-user-canary"
const p319Body = "p319-body-canary"

func p319SMTP(t *testing.T, stage string) (string, string) {
	t.Helper()
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
				if stage == "starttls" {
					if !send("250-fake\r\n250 STARTTLS") {
						return
					}
					continue
				}
				if !send("250-fake\r\n250 AUTH PLAIN") {
					return
				}
			case strings.HasPrefix(line, "STARTTLS"):
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

func TestP319HTTPAndLog(t *testing.T) {
	for _, stage := range []string{"auth", "malformed_auth", "mail", "data_end"} {
		t.Run(stage, func(t *testing.T) {
			host, port := p319SMTP(t, stage)
			req := map[string]string{"host": host, "port": port, "security": "auto_starttls", "username": p319User, "password": p319Password, "from_addr": "f@example.com", "to_addr": "t@example.com", "subject": "subject", "body": p319Body}
			payload, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			env := newTestEnv(t)
			before, err := env.store.GetAlertEmail()
			if err != nil {
				t.Fatal(err)
			}
			applies := env.applyCount()
			var logs bytes.Buffer
			prev := slog.Default()
			oldWriter, oldFlags := log.Writer(), log.Flags()
			broadcaster := NewLogBroadcaster("info")
			slog.SetDefault(slog.New(slog.NewMultiHandler(slog.NewTextHandler(&logs, nil), broadcaster)))
			defer func() { slog.SetDefault(prev); log.SetOutput(oldWriter); log.SetFlags(oldFlags) }()
			srv := httptest.NewServer(http.HandlerFunc(env.deps.handleTestEmail))
			defer srv.Close()
			resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			result, err := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" {
				t.Error("HTTP contract changed")
			}
			var dto testEmailResponse
			if err := json.Unmarshal(result, &dto); err != nil {
				t.Fatal(err)
			}
			if dto.Success || dto.Error == "" {
				t.Error("missing failure result")
			}
			p319AssertSafe(t, string(result))
			p319AssertSafe(t, logs.String())
			history, _, _, unsub := broadcaster.Subscribe("")
			defer unsub()
			select {
			case entry := <-history:
				p319AssertSafe(t, entry.Line)
			case <-time.After(time.Second):
				t.Error("missing replay")
			}
			after, err := env.store.GetAlertEmail()
			if err != nil {
				t.Fatal(err)
			}
			if *before != *after || env.applyCount() != applies {
				t.Error("test email changed configuration")
			}
		})
	}
}
