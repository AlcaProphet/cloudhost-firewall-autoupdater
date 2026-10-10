package config

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

// 冻结当前已含主题/正文但缺安全模式的邮件表；不引用候选 DDL。
const smtpCurrentEmailTable = `CREATE TABLE alert_email (
 id INTEGER PRIMARY KEY DEFAULT 1,
 enabled INTEGER NOT NULL DEFAULT 0,
 host TEXT NOT NULL DEFAULT '',
 port TEXT NOT NULL DEFAULT '587',
 username TEXT NOT NULL DEFAULT '',
 password TEXT NOT NULL DEFAULT '',
 from_addr TEXT NOT NULL DEFAULT '',
 to_addr TEXT NOT NULL DEFAULT '',
 subject TEXT NOT NULL DEFAULT '[FWAlizer] 告警通知',
 body TEXT NOT NULL DEFAULT 'FWAlizer 检测到运行异常，请检查同步日志。'
);`

func TestSMTPSecurityMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current-old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(smtpCurrentEmailTable + `INSERT INTO alert_email(id,enabled,host,port,username,password,from_addr,to_addr,subject,body) VALUES(1,1,'smtp.keep','465','u','secret','from','to','subject keep','body keep');CREATE TABLE alert_webhook(id INTEGER PRIMARY KEY,enabled INTEGER,url TEXT,channel TEXT);INSERT INTO alert_webhook VALUES(1,1,'https://keep.invalid','feishu');`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	}()

	want := AlertEmailConfig{Enabled: true, Host: "smtp.keep", Port: "465", Security: DefaultSMTPSecurity, Username: "u", Password: "secret", FromAddr: "from", ToAddr: "to", Subject: "subject keep", Body: "body keep"}
	for _, mode := range []string{DefaultSMTPSecurity, SMTPSecurityImplicitTLS, SMTPSecuritySTARTTLS} {
		got, err := store.GetAlertEmail()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*got, want) {
			t.Fatalf("迁移改变已有邮件配置: %+v", got)
		}
		hook, err := store.GetAlertWebhook()
		if err != nil || !hook.Enabled || hook.URL != "https://keep.invalid" || hook.Channel != "feishu" {
			t.Fatalf("迁移改变 Webhook: %+v %v", hook, err)
		}
		want.Security = mode
		if err := store.SaveAlertEmail(&want); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenStore(path)
		if err != nil {
			t.Fatal(err)
		}
		store = reopened
		// 真实重启再次执行生产迁移，已选模式与全部旧字段保持。
		got, err = store.GetAlertEmail()
		if err != nil || !reflect.DeepEqual(*got, want) {
			t.Fatalf("重启改变配置: %+v %v", got, err)
		}
	}
	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResetAllTx(context.Background(), tx); err != nil {
		if rb := tx.Rollback(); rb != nil {
			t.Error(rb)
		}
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAlertEmail()
	if err != nil || got.Security != DefaultSMTPSecurity || got.Port != "587" || got.Enabled {
		t.Fatalf("reset 默认值异常: %+v %v", got, err)
	}
}

func TestSMTPSecurityNewStoreDefaults(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg, err := store.GetAlertEmail()
	if err != nil || cfg.Security != DefaultSMTPSecurity || cfg.Port != "587" || cfg.Enabled {
		t.Fatalf("新库默认值异常: %+v %v", cfg, err)
	}
}

func TestSMTPSecurityValidation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, mode := range []string{"auto_starttls", "starttls", "implicit_tls", "", " STARTTLS ", "STARTTLS", "plain", "mode-secret"} {
			cfg := AlertEmailConfig{Enabled: enabled, Host: "smtp.test", Port: "465", Security: mode, FromAddr: "f", ToAddr: "t", Subject: "s"}
			got, err := NormalizeAlertEmail(cfg)
			valid := mode == "auto_starttls" || mode == "starttls" || mode == "implicit_tls"
			if valid && (err != nil || got.Security != mode || got.Port != "465") {
				t.Fatalf("合法模式未保留: %+v %v", got, err)
			}
			if !valid {
				requireInvalid(t, err, "email.security")
			}
		}
	}
}
