package app

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/version"

	_ "modernc.org/sqlite"
)

// RunCLI 处理子命令，返回 true 表示已处理（不需进入主流程）
func RunCLI(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[1] {
	case "version":
		fmt.Printf("fwalizer %s\n", version.Version)
		return true
	case "validate":
		path := ".env"
		if len(args) >= 3 {
			path = args[2]
		}
		cfg, err := config.LoadEnv(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "解析失败: %v\n", err)
			os.Exit(1)
		}
		if err := cfg.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "校验失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("配置有效: %d 个目标, %d 条规则\n", len(cfg.Targets), len(cfg.DomainRules))
		return true

	case "backup":
		dataDir := config.GetDataDir()
		src := filepath.Join(dataDir, "config.db")
		ts := time.Now().Format("20060102_150405")
		dst := filepath.Join(dataDir, fmt.Sprintf("config.db.bak.%s", ts))
		if err := copyFile(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "备份失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("备份成功: %s\n", dst)
		cleanOldBackups(dataDir, 5)
		return true

	case "restore":
		if len(args) < 3 {
			fmt.Fprintf(os.Stderr, "用法: fwalizer restore <备份文件路径>\n")
			os.Exit(1)
		}
		backupFile := args[2]
		dataDir := config.GetDataDir()
		dst := filepath.Join(dataDir, "config.db")
		if err := verifyBackup(backupFile); err != nil {
			fmt.Fprintf(os.Stderr, "备份文件校验失败: %v\n", err)
			os.Exit(1)
		}
		if err := copyFile(backupFile, dst); err != nil {
			fmt.Fprintf(os.Stderr, "恢复失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("恢复成功，请重启 FWAlizer")
		return true
	}
	return false
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// cleanOldBackups 保留最新 N 个备份，删除其余
func cleanOldBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "config.db.bak.") {
			backups = append(backups, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))
	if len(backups) <= keep {
		return
	}
	for _, name := range backups[keep:] {
		os.Remove(filepath.Join(dir, name))
	}
}

// verifyBackup 校验备份文件完整性（SQLite PRAGMA integrity_check）
func verifyBackup(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("打开备份文件失败: %w", err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("执行完整性检查失败: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("数据库完整性检查未通过: %s", result)
	}
	return nil
}
