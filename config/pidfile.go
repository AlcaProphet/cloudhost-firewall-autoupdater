package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

// GetPidFilePath 返回 pidfile 路径。
func GetPidFilePath(dataDir string) string {
	return filepath.Join(dataDir, "fwalizer.pid")
}

// WritePidFile 取得同一数据目录的独占锁，PID 仅供诊断。清理时保留文件以避免 inode 竞态。
func WritePidFile(path string) (cleanup func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开 pidfile 失败: %w", err)
	}
	// 闭包持有文件对象，防止 GC 在应用运行期间提前关闭锁的文件描述符。
	closeFile := func() {
		if err := f.Close(); err != nil {
			slog.Warn("关闭 pidfile 失败", "error", err)
		}
	}
	if err := lockPidFile(f); err != nil {
		closeFile()
		return nil, err
	}
	// 只有持锁者可以改写诊断内容，不能在 OpenFile 时使用 O_TRUNC。
	if err := f.Truncate(0); err != nil {
		closeFile()
		return nil, fmt.Errorf("截断 pidfile 失败: %w", err)
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid()) + "\n"); err != nil {
		closeFile()
		return nil, fmt.Errorf("写入 pidfile 失败: %w", err)
	}
	return closeFile, nil
}
