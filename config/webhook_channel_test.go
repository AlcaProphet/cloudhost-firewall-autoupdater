package config

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestI8113WebhookInitialization(t *testing.T) {
	cases := []struct{ name, ddl, want string }{
		{"fresh", "", ""},
		{"legacy_empty", `CREATE TABLE alert_webhook(id INTEGER PRIMARY KEY DEFAULT 1,enabled INTEGER DEFAULT 0,url TEXT DEFAULT '');`, ""},
		{"legacy_missing_column", `CREATE TABLE alert_webhook(id INTEGER PRIMARY KEY DEFAULT 1,enabled INTEGER DEFAULT 0,url TEXT DEFAULT ''); INSERT INTO alert_webhook(id,enabled,url) VALUES(1,1,'https://example.invalid/keep');`, "dingtalk"},
		{"old_empty_table", `CREATE TABLE alert_webhook(id INTEGER PRIMARY KEY DEFAULT 1,enabled INTEGER DEFAULT 0,url TEXT DEFAULT '',channel TEXT DEFAULT 'dingtalk');`, ""},
		{"old_configured", `CREATE TABLE alert_webhook(id INTEGER PRIMARY KEY DEFAULT 1,enabled INTEGER DEFAULT 0,url TEXT DEFAULT '',channel TEXT DEFAULT 'dingtalk'); INSERT INTO alert_webhook(id,enabled,url,channel) VALUES(1,1,'https://example.invalid/keep','feishu');`, "feishu"},
	}
	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db")
			if v.ddl != "" {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec(v.ddl); err != nil {
					t.Fatal(err)
				}
				if err = db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			s, err := OpenStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			}()
			c, err := s.GetAlertWebhook()
			if err != nil {
				t.Fatal(err)
			}
			if c.Channel != v.want {
				t.Errorf("open=%q want=%q", c.Channel, v.want)
			}
			if v.name == "fresh" || v.name == "legacy_empty" || v.name == "legacy_missing_column" {
				var dflt string
				if err := s.db.QueryRow(`SELECT dflt_value FROM pragma_table_info('alert_webhook') WHERE name='channel'`).Scan(&dflt); err != nil {
					t.Fatal(err)
				}
				if dflt != "''" {
					t.Errorf("new column default=%q want empty string", dflt)
				}
			}
			if v.name == "legacy_missing_column" || v.name == "old_configured" {
				if !c.Enabled || c.URL != "https://example.invalid/keep" {
					t.Errorf("existing enable/URL changed: %+v", c)
				}
			}
			if err := s.WithTransaction(func(tx *sql.Tx) error { return s.ResetAllTx(context.Background(), tx) }); err != nil {
				t.Fatal(err)
			}
			c, err = s.GetAlertWebhook()
			if err != nil {
				t.Fatal(err)
			}
			if c.Channel != "" || c.Enabled || c.URL != "" {
				t.Errorf("reset=%+v", c)
			}
		})
	}
	for _, c := range []AlertWebhookConfig{{}, {Channel: " "}, {Channel: "feishu"}, {Channel: "slack", URL: "draft"}} {
		if _, err := NormalizeAlertWebhook(c); err != nil {
			t.Errorf("disabled %+v: %v", c, err)
		}
	}
	for _, c := range []AlertWebhookConfig{{Enabled: true}, {Enabled: true, URL: "https://example.invalid"}, {Channel: "wecom"}, {Enabled: true, Channel: "slack"}} {
		if _, err := NormalizeAlertWebhook(c); err == nil {
			t.Errorf("must reject=%+v", c)
		}
	}
}

// TestI8113MigrationRollbackAndRestart 缺列迁移失败须回滚 ALTER，重试与再次启动不得改写用户渠道。
func TestI8113MigrationRollbackAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	ddl := `CREATE TABLE alert_webhook(id INTEGER PRIMARY KEY DEFAULT 1,enabled INTEGER DEFAULT 0,url TEXT DEFAULT ''); INSERT INTO alert_webhook(id,enabled,url) VALUES(1,1,'https://example.invalid/keep'); CREATE TRIGGER fail_channel BEFORE UPDATE ON alert_webhook BEGIN SELECT RAISE(ABORT,'fixture failure'); END;`
	if _, err = db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path)
	if err == nil {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		t.Fatal("must fail")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var n, enabled int
	var url string
	if err = db.QueryRow(`SELECT count(*) FROM pragma_table_info('alert_webhook') WHERE name='channel'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("ALTER not rolled back")
	}
	if err = db.QueryRow(`SELECT enabled,url FROM alert_webhook WHERE id=1`).Scan(&enabled, &url); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 || url != "https://example.invalid/keep" {
		t.Fatal("old config changed on failure")
	}
	if _, err = db.Exec(`DROP TRIGGER fail_channel`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE alert_webhook SET enabled=1, channel='slack' WHERE id=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	c, err := s.GetAlertWebhook()
	if err != nil {
		t.Fatal(err)
	}
	if c.Channel != "slack" || !c.Enabled || c.URL != "https://example.invalid/keep" {
		t.Errorf("restart lost config=%+v", c)
	}
}
