//go:build linux || darwin

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// lockPidFile 非阻塞取得内核锁，诊断内容不参与互斥判定。
func lockPidFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		// 使用同一文件描述符有界读取；竞争者可能遇到持锁者尚未写完 PID 的窗口。
		var buf [64]byte
		n, readErr := f.ReadAt(buf[:], 0)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("FWAlizer 已在运行（同一数据目录已被锁定），诊断 PID 不可用，请先停止现有实例")
		}
		if n > 0 {
			if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(buf[:n]))); parseErr == nil && pid > 0 {
				return fmt.Errorf("FWAlizer 已在运行（同一数据目录已被锁定，诊断 PID: %d），请先停止现有实例", pid)
			}
		}
		return fmt.Errorf("FWAlizer 已在运行（同一数据目录已被锁定），请先停止现有实例")
	}
	return fmt.Errorf("锁定 pidfile 失败: %w", err)
}
