package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// 部署参数默认值与边界（Build6 §1.3）
const (
	DefaultWebUIHost = "127.0.0.1"
	DefaultWebUIPort = 60200
)

// DeploymentConfig 进程部署参数。
//
// 只在启动时读取一次，不写入 SQLite、不进入配置包、不参与热重载，
// 也不接受 TARGETS 等业务环境变量 override。
type DeploymentConfig struct {
	DataDir string // SQLite、pidfile 和持久化数据目录
	Host    string // HTTP 监听地址
	Port    int    // HTTP 监听端口（1～65535）
}

// LoadDeploymentConfig 读取并校验三个部署环境变量。
//
//   - FWALIZER_DATA_DIR：未设置或 Trim 后为空时使用平台默认目录
//   - WEBUI_HOST：默认 127.0.0.1
//   - WEBUI_PORT：默认 60200，只接受十进制整数 1～65535
func LoadDeploymentConfig() (DeploymentConfig, error) {
	cfg := DeploymentConfig{
		DataDir: DefaultDataDir(),
		Host:    DefaultWebUIHost,
		Port:    DefaultWebUIPort,
	}

	if dir := strings.TrimSpace(os.Getenv("FWALIZER_DATA_DIR")); dir != "" {
		cfg.DataDir = dir
	}
	if host := strings.TrimSpace(os.Getenv("WEBUI_HOST")); host != "" {
		cfg.Host = host
	}
	if raw := strings.TrimSpace(os.Getenv("WEBUI_PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil {
			return DeploymentConfig{}, fmt.Errorf("WEBUI_PORT 必须为十进制整数: %q", raw)
		}
		if port < 1 || port > 65535 {
			return DeploymentConfig{}, fmt.Errorf("WEBUI_PORT 必须在 1～65535 范围内: %d", port)
		}
		cfg.Port = port
	}

	return cfg, nil
}

// DefaultDataDir 返回平台默认数据目录。
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// 回退到当前目录（极端情况）
		return "."
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "fwalizer")
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			appdata = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appdata, "fwalizer")
	default:
		return filepath.Join(home, ".config", "fwalizer")
	}
}
