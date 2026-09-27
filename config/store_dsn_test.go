package config

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── Issue6 A4：busy_timeout 必须覆盖每条物理连接；路径不得被截断 ───

// TestOpenStoreBusyTimeoutOnEveryConnection 每条物理连接都必须拿到 5000ms。
//
// 判别性：修复前只在打开后执行一次 `PRAGMA busy_timeout=5000`，该 PRAGMA 是连接级，
// database/sql 之后新建的连接为 0——即「恰为 1×5000 + 3×0」。改用 DSN `_pragma=`
// 后由驱动在每条新连接上执行，四条并发持留连接必须全部为 5000。
func TestOpenStoreBusyTimeoutOnEveryConnection(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "busy.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer s.Close()

	const conns = 4
	held := make([]*sql.Conn, 0, conns)
	for i := 0; i < conns; i++ {
		c, err := s.db.Conn(context.Background())
		if err != nil {
			t.Fatalf("获取第 %d 条连接失败: %v", i+1, err)
		}
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()

	zero := 0
	for i, c := range held {
		var got int
		if err := c.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&got); err != nil {
			t.Fatalf("第 %d 条连接读取 busy_timeout 失败: %v", i+1, err)
		}
		if got != 5000 {
			t.Errorf("第 %d 条连接 busy_timeout = %d, want 5000（修复前除首条外均为 0）", i+1, got)
			if got == 0 {
				zero++
			}
		}
	}
	if zero == conns {
		t.Fatal("所有连接 busy_timeout 均为 0：DSN `_pragma` 未生效")
	}
}

// TestOpenStorePathsWithSpecialCharacters 含空格/`#`/`?`/非 ASCII 的路径必须落到预期文件。
//
// 判别性：修复前用纯路径 DSN，驱动在非 `file:` 前缀时对 `?` 执行 `dsn = dsn[:pos]`，
// 因此 `/data/a?b.db` 会被静默打开成 `/data/a`（写到另一个文件且无任何报错）。
func TestOpenStorePathsWithSpecialCharacters(t *testing.T) {
	dir := t.TempDir()

	names := []string{
		"plain.db",
		"with space.db",
		"hash#name.db",
		"question?name.db",
		"中文名称.db",
		"both?and#chars.db",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			s, err := OpenStore(path)
			if err != nil {
				t.Fatalf("OpenStore(%q) 失败: %v", path, err)
			}
			defer s.Close()

			// 业务写入正常，证明连接可用
			if err := s.SetSetting("theme", "dark"); err != nil {
				t.Fatalf("写入设置失败: %v", err)
			}

			// 文件必须恰好建在预期路径
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("预期数据库文件不存在: %v（修复前 `?` 之后的路径会被截断）", err)
			}
		})
	}
}

// TestOpenStoreRelativePathUsesAbsFileURI 相对路径必须可打开。
//
// 判别性：`url.URL{Scheme:"file", Path: rel}` 会输出 `file://rel`，SQLite 把它当
// authority 并报 `invalid uri authority`；因此实现必须先 filepath.Abs。
func TestOpenStoreRelativePathUsesAbsFileURI(t *testing.T) {
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取当前目录失败: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	s, err := OpenStore(filepath.Join(".", "relative.db"))
	if err != nil {
		t.Fatalf("相对路径 OpenStore 失败: %v", err)
	}
	defer s.Close()

	if err := s.SetSetting("theme", "light"); err != nil {
		t.Fatalf("相对路径写入失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "relative.db")); err != nil {
		t.Fatalf("相对路径数据库未落在预期位置: %v", err)
	}
}

// TestSQLiteDSNShape DSN 形状固定：file: 前缀 + 仅 busy_timeout 一个 _pragma。
func TestSQLiteDSNShape(t *testing.T) {
	dsn, err := sqliteDSN(filepath.Join(t.TempDir(), "a?b#c.db"))
	if err != nil {
		t.Fatalf("sqliteDSN 失败: %v", err)
	}
	if !strings.HasPrefix(dsn, "file:") {
		t.Errorf("DSN 必须以 file: 开头，实际 %q", dsn)
	}
	if strings.Count(dsn, "_pragma=") != 1 {
		t.Errorf("_pragma 只能出现一次，实际 %q", dsn)
	}
	if !strings.Contains(dsn, "_pragma=busy_timeout(5000)") {
		t.Errorf("_pragma 必须只承载 busy_timeout(5000)，实际 %q", dsn)
	}
	for _, forbidden := range []string{"journal_mode", "_txlock", "_pragma=busy_timeout(5000)&"} {
		if strings.Contains(dsn, forbidden) {
			t.Errorf("DSN 不得包含 %q：%q", forbidden, dsn)
		}
	}
	// 路径里的特殊字符必须被转义（否则会被 URI 解析成 authority/fragment）
	if strings.Contains(dsn, "a?b#c.db") {
		t.Errorf("DSN 未转义路径中的 ? 与 #：%q", dsn)
	}
	if !strings.Contains(dsn, "%3F") || !strings.Contains(dsn, "%23") {
		t.Errorf("DSN 必须把 ? 与 # 转义为 %%3F/%%23：%q", dsn)
	}
}

// TestBusyTimeoutWaitsInsteadOfFailingImmediately 写锁竞争必须等待（约 5s）而不是立即失败。
//
// 按 Issue6 A4 要求：竞争用例走 `BEGIN IMMEDIATE`（WAL 下 deferred 事务先读后写会得到
// SQLITE_BUSY_SNAPSHOT，不受 busy handler 约束，会让修复后用例仍然失败）。
func TestBusyTimeoutWaitsInsteadOfFailingImmediately(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过约 5s 的锁等待用例")
	}

	s, err := OpenStore(filepath.Join(t.TempDir(), "contend.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer s.Close()

	holder, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("获取持有连接失败: %v", err)
	}
	defer holder.Close()

	// 持有写锁
	if _, err := holder.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("持有连接开启写事务失败: %v", err)
	}
	defer func() { _, _ = holder.ExecContext(context.Background(), "ROLLBACK") }()

	// 另一条连接尝试写入：必须等待约 5s 后返回 SQLITE_BUSY，而不是立即失败
	waiter, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("获取等待连接失败: %v", err)
	}
	defer waiter.Close()

	start := time.Now()
	_, execErr := waiter.ExecContext(context.Background(), "INSERT INTO settings (key, value) VALUES ('k', 'v')")
	elapsed := time.Since(start)

	if execErr == nil {
		t.Fatal("写锁被占用时另一条连接的写入不应成功")
	}
	if elapsed < 4*time.Second {
		t.Fatalf("写入在 %v 就失败：busy_timeout 未生效（修复前非首条连接为 0，会立即 SQLITE_BUSY）", elapsed)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("写入等待 %v 远超 busy_timeout(5000ms)", elapsed)
	}
	t.Logf("写锁竞争等待 %v 后返回预期错误: %v", elapsed.Round(time.Millisecond), execErr)
}

// TestConcurrentConnectionsAllHaveBusyTimeout 并发获取连接时每条都带 busy_timeout。
func TestConcurrentConnectionsAllHaveBusyTimeout(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "conc.db"))
	if err != nil {
		t.Fatalf("OpenStore 失败: %v", err)
	}
	defer s.Close()

	const n = 8
	results := make([]int, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := s.db.Conn(context.Background())
			if err != nil {
				errs[idx] = err
				return
			}
			defer c.Close()
			errs[idx] = c.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&results[idx])
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("第 %d 条并发连接失败: %v", i+1, errs[i])
		}
		if results[i] != 5000 {
			t.Errorf("第 %d 条并发连接 busy_timeout = %d, want 5000", i+1, results[i])
		}
	}
}
