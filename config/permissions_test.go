//go:build linux || darwin

package config

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// assertPrivateMode 检查精确位，不把“当前用户能读取”当作敏感文件权限通过。
func assertPrivateMode(t *testing.T, path string, want os.FileMode) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Errorf("%s mode=%04o want=%04o", filepath.Base(path), info.Mode().Perm(), want)
	}
	return info
}
func closePermissionStore(t *testing.T, s *Store) {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
func openPermissionStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestStorePermissions 每个 umask 在独立进程设置，避免干扰并发测试或调用方。
func TestStorePermissions(t *testing.T) {
	for _, mask := range []string{"0000", "0022", "0077", "0777"} {
		t.Run(mask, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStorePermissionsChild$", "-test.v")
			cmd.Env = append(os.Environ(), "FWALIZER_TEST_PERMISSION_MASK="+mask)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("umask=%s: %v\n%s", mask, err, out)
			}
		})
	}
}
func TestStorePermissionsChild(t *testing.T) {
	raw := os.Getenv("FWALIZER_TEST_PERMISSION_MASK")
	if raw == "" {
		t.Skip("仅子进程执行")
	}
	mask, err := strconv.ParseUint(raw, 8, 32)
	if err != nil {
		t.Fatal(err)
	}
	// TempDir 会先创建父子目录；先创建它，再设置极严 umask，避免夹具自身不可遍历。
	dir := t.TempDir()
	previous := syscall.Umask(int(mask))
	defer syscall.Umask(previous)
	path := filepath.Join(dir, "配置 #?%.db")
	s := openPermissionStore(t, path)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		assertPrivateMode(t, path+suffix, 0600)
	}
	// 强制两条实际连接，再证明全部连接关闭后的辅助文件会安全重建。
	c1, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c2.ExecContext(context.Background(), "INSERT INTO settings(key,value) VALUES('permission_probe','persisted')"); err != nil {
		t.Fatal(err)
	}
	if err := c1.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c2.Close(); err != nil {
		t.Fatal(err)
	}
	s.db.SetMaxIdleConns(0)
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("连接全部关闭后辅助文件仍存在: %s err=%v", suffix, err)
		}
	}
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := conn.QueryRowContext(context.Background(), "SELECT value FROM settings WHERE key='permission_probe'").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "persisted" {
		t.Fatalf("重建后配置丢失: %q", value)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		assertPrivateMode(t, path+suffix, 0600)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	closePermissionStore(t, s)
	// 模拟旧主库模式，重启不能截断已有配置。
	for _, wide := range []os.FileMode{0644, 0666} {
		if err := os.Chmod(path, wide); err != nil {
			t.Fatal(err)
		}
		s = openPermissionStore(t, path)
		if err := s.db.QueryRow("SELECT value FROM settings WHERE key='permission_probe'").Scan(&value); err != nil {
			t.Fatal(err)
		}
		if value != "persisted" {
			t.Fatal("权限迁移截断主库")
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			assertPrivateMode(t, path+suffix, 0600)
		}
		closePermissionStore(t, s)
	}
	// 非正常收尾留下真实非空 WAL，不能用空占位辅助文件代替恢复验收。
	for _, wide := range []string{"0644", "0666"} {
		crash := filepath.Join(dir, "crash-"+wide+".db")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStorePermissionsCrashChild$")
		cmd.Env = append(os.Environ(), "FWALIZER_TEST_PERMISSION_CRASH="+crash, "FWALIZER_TEST_PERMISSION_WIDE="+wide)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("WAL夹具失败: %v\n%s", err, out)
		}
		wal, err := os.Stat(crash + "-wal")
		if err != nil || wal.Size() == 0 {
			t.Fatalf("必须留下非空WAL: info=%v err=%v", wal, err)
		}
		s = openPermissionStore(t, crash)
		if err := s.db.QueryRow("SELECT value FROM settings WHERE key='wal_probe'").Scan(&value); err != nil {
			t.Fatal(err)
		}
		if value != "committed-in-wal" {
			t.Fatalf("WAL恢复丢失配置: %q", value)
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			assertPrivateMode(t, crash+suffix, 0600)
		}
		closePermissionStore(t, s)
	}
	// 旧锁内容、宽模式与实际内核锁独立；竞争者不得迁移持锁者文件。
	for _, wide := range []os.FileMode{0644, 0666} {
		lock := GetPidFilePath(dir)
		content := []byte("invalid old pid")
		if err := os.WriteFile(lock, content, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(lock, wide); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(lock)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(lock, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		cleanup, lockErr := WritePidFile(lock)
		if lockErr == nil {
			cleanup()
			t.Fatal("竞争者绕过锁")
		}
		if cleanup != nil || !strings.Contains(lockErr.Error(), "FWAlizer 已在运行") {
			t.Fatalf("错误锁语义: %v", lockErr)
		}
		assertPrivateMode(t, lock, wide)
		data, err := os.ReadFile(lock)
		if err != nil || string(data) != string(content) {
			t.Fatalf("竞争者改写内容: %q err=%v", data, err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		cleanup, err = WritePidFile(lock)
		if err != nil {
			t.Fatal(err)
		}
		after := assertPrivateMode(t, lock, 0600)
		cleanup()
		if !os.SameFile(before, after) {
			t.Fatal("迁移替换了锁文件 inode")
		}
		if _, err := os.Stat(lock); err != nil {
			t.Fatal("释放锁删除了文件", err)
		}
	}
}
func TestStorePermissionsCrashChild(t *testing.T) {
	path := os.Getenv("FWALIZER_TEST_PERMISSION_CRASH")
	if path == "" {
		t.Skip("仅子进程执行")
	}
	mode, err := strconv.ParseUint(os.Getenv("FWALIZER_TEST_PERMISSION_WIDE"), 8, 32)
	if err != nil {
		t.Fatal(err)
	}
	syscall.Umask(0)
	s := openPermissionStore(t, path)
	if err := s.SetSetting("wal_probe", "committed-in-wal"); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(path+suffix, os.FileMode(mode)); err != nil {
			t.Fatal(err)
		}
	}
	// 刻意不 Close：保留真实 WAL，由父进程验证恢复及迁移。
	os.Exit(0)
}

// TestStoreAuxPermissionsErrors 只忽略 ENOENT；其他路径错误不能当作“没有辅助文件”。
func TestStoreAuxPermissionsErrors(t *testing.T) {
	dir := t.TempDir()
	if err := chmodStoreAuxFiles(filepath.Join(dir, "missing.db")); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "not-directory")
	if err := os.WriteFile(parent, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	err := chmodStoreAuxFiles(filepath.Join(parent, "config.db"))
	if !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("非ENOENT错误丢失: %v", err)
	}
}

// p312Connector 用标准 driver 接缝在 Commit 后构造辅助文件路径故障，无生产测试开关。
type p312Connector struct {
	p310Connector
	afterCommit func() error
	commits     int
}

func (c *p312Connector) Connect(context.Context) (driver.Conn, error) {
	return &p312Conn{p310Conn: p310Conn{&c.p310Connector}, owner: c}, nil
}

type p312Conn struct {
	p310Conn
	owner *p312Connector
}

func (c *p312Conn) Begin() (driver.Tx, error) { return &p312Tx{owner: c.owner}, nil }
func (c *p312Conn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return p312EmptyRows{}, nil
}

type p312EmptyRows struct{}

func (p312EmptyRows) Columns() []string {
	return []string{"cid", "name", "type", "notnull", "default", "pk"}
}
func (p312EmptyRows) Close() error              { return nil }
func (p312EmptyRows) Next([]driver.Value) error { return io.EOF }

type p312Tx struct{ owner *p312Connector }

func (tx *p312Tx) Commit() error {
	tx.owner.commits++
	if tx.owner.afterCommit != nil {
		return tx.owner.afterCommit()
	}
	return nil
}
func (tx *p312Tx) Rollback() error { tx.owner.rollbacks++; return nil }

// TestStorePostInitPermissionFailureCloses 初始化后的权限失败也必须关闭且保留主/关闭错误。
func TestStorePostInitPermissionFailureCloses(t *testing.T) {
	for _, tc := range []struct {
		name            string
		fail, closeFail bool
	}{{"success", false, false}, {"permission_failure", true, false}, {"permission_and_close_failure", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "files")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			c := &p312Connector{}
			if tc.closeFail {
				c.closing = errors.New("injected close error")
			}
			if tc.fail {
				c.afterCommit = func() error {
					if err := os.Rename(parent, parent+"-old"); err != nil {
						return err
					}
					return os.WriteFile(parent, []byte("not-directory"), 0600)
				}
			}
			db := sql.OpenDB(c)
			s, err := openStoreDB(db, filepath.Join(parent, "config.db"))
			if !tc.fail {
				if err != nil || s == nil || c.closes != 0 {
					t.Fatalf("成功被误关闭: store=%v err=%v closes=%d", s, err, c.closes)
				}
				closePermissionStore(t, s)
				return
			}
			if s != nil || !errors.Is(err, syscall.ENOTDIR) || c.commits != 1 || c.closes != 1 || c.rollbacks != 0 {
				t.Fatalf("权限错误收尾失真: store=%v err=%v commits=%d closes=%d rollbacks=%d", s, err, c.commits, c.closes, c.rollbacks)
			}
			if tc.closeFail && !errors.Is(err, c.closing) {
				t.Fatalf("关闭错误丢失: %v", err)
			}
			var pathErr *os.PathError
			if !errors.As(err, &pathErr) || pathErr.Op != "chmod" {
				t.Fatalf("权限错误类型丢失: %v", err)
			}
			if tc.closeFail && strings.Index(err.Error(), "辅助文件") > strings.Index(err.Error(), "关闭数据库") {
				t.Fatal("关闭错误遮蔽主错误")
			}
			if err := db.Ping(); err == nil {
				t.Fatal("失败数据库仍可用")
			}
		})
	}
}

// TestStorePrepareFailure 不可打开主库时返回 nil，原有文件内容不变。
func TestStorePrepareFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent-file")
	if err := os.WriteFile(parent, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(filepath.Join(parent, "config.db"))
	if s != nil || !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("预创建失败仍返回Store: store=%v err=%v", s, err)
	}
	raw, err := os.ReadFile(parent)
	if err != nil || string(raw) != "keep" {
		t.Fatal(fmt.Sprintf("父文件被改动: %q %v", raw, err))
	}
}
