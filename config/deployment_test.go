package config

import (
	"strings"
	"testing"
)

// TestLoadDeploymentConfig_Defaults 三个部署变量全部未设置时的默认值
func TestLoadDeploymentConfig_Defaults(t *testing.T) {
	t.Setenv("FWALIZER_DATA_DIR", "")
	// t.Setenv 置为空字符串等价于未设置（os.Getenv 无法区分）
	t.Setenv("WEBUI_HOST", "")
	t.Setenv("WEBUI_PORT", "")

	cfg, err := LoadDeploymentConfig()
	if err != nil {
		t.Fatalf("默认部署参数不应报错: %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.Port != 60200 {
		t.Errorf("Port = %d, want 60200", cfg.Port)
	}
	if cfg.DataDir != DefaultDataDir() {
		t.Errorf("DataDir = %q, want 平台默认目录 %q", cfg.DataDir, DefaultDataDir())
	}
}

// TestLoadDeploymentConfig_BlankDataDirTreatedAsUnset 空白 FWALIZER_DATA_DIR 按未设置处理
func TestLoadDeploymentConfig_BlankDataDirTreatedAsUnset(t *testing.T) {
	for _, blank := range []string{"", " ", "\t", "  \n  "} {
		t.Setenv("FWALIZER_DATA_DIR", blank)
		cfg, err := LoadDeploymentConfig()
		if err != nil {
			t.Fatalf("FWALIZER_DATA_DIR=%q 不应报错: %v", blank, err)
		}
		if cfg.DataDir != DefaultDataDir() {
			t.Errorf("FWALIZER_DATA_DIR=%q 时 DataDir = %q, want 平台默认目录 %q", blank, cfg.DataDir, DefaultDataDir())
		}
	}
}

// TestLoadDeploymentConfig_DataDirTrimmed 非空数据目录保留 Trim 后的值
func TestLoadDeploymentConfig_DataDirTrimmed(t *testing.T) {
	t.Setenv("FWALIZER_DATA_DIR", "  /tmp/fwalizer-data  ")

	cfg, err := LoadDeploymentConfig()
	if err != nil {
		t.Fatalf("合法数据目录不应报错: %v", err)
	}
	if cfg.DataDir != "/tmp/fwalizer-data" {
		t.Errorf("DataDir = %q, want /tmp/fwalizer-data", cfg.DataDir)
	}
}

// TestLoadDeploymentConfig_HostIndependent WEBUI_HOST 独立生效
func TestLoadDeploymentConfig_HostIndependent(t *testing.T) {
	t.Setenv("FWALIZER_DATA_DIR", "")
	t.Setenv("WEBUI_HOST", "0.0.0.0")
	t.Setenv("WEBUI_PORT", "")

	cfg, err := LoadDeploymentConfig()
	if err != nil {
		t.Fatalf("合法监听地址不应报错: %v", err)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.Port != 60200 {
		t.Errorf("仅设置 WEBUI_HOST 时 Port = %d, want 60200", cfg.Port)
	}
}

// TestLoadDeploymentConfig_PortIndependent WEBUI_PORT 独立生效
func TestLoadDeploymentConfig_PortIndependent(t *testing.T) {
	t.Setenv("FWALIZER_DATA_DIR", "")
	t.Setenv("WEBUI_HOST", "")
	t.Setenv("WEBUI_PORT", "61234")

	cfg, err := LoadDeploymentConfig()
	if err != nil {
		t.Fatalf("合法端口不应报错: %v", err)
	}
	if cfg.Port != 61234 {
		t.Errorf("Port = %d, want 61234", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("仅设置 WEBUI_PORT 时 Host = %q, want 127.0.0.1", cfg.Host)
	}
}

// TestLoadDeploymentConfig_PortBoundaries 端口 1 和 65535 有效，并允许首尾空白
func TestLoadDeploymentConfig_PortBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{"1", 1},
		{"65535", 65535},
		{"  60200  ", 60200},
	} {
		t.Setenv("WEBUI_PORT", tc.raw)
		cfg, err := LoadDeploymentConfig()
		if err != nil {
			t.Fatalf("WEBUI_PORT=%q 应有效: %v", tc.raw, err)
		}
		if cfg.Port != tc.want {
			t.Errorf("WEBUI_PORT=%q → Port = %d, want %d", tc.raw, cfg.Port, tc.want)
		}
	}
}

// TestLoadDeploymentConfig_InvalidPort 非法端口必须启动失败
func TestLoadDeploymentConfig_InvalidPort(t *testing.T) {
	for _, raw := range []string{
		"0",       // 下界外
		"65536",   // 上界外
		"-1",      // 负数
		"abc",     // 非数字
		"1e3",     // 非十进制整数写法
		"80 80",   // 夹杂空格
		"60_200",  // 数字分隔符不属于十进制文本
		"0x1F90",  // 十六进制
		"60200.0", // 小数
		"1,2",     // 多值
	} {
		t.Setenv("WEBUI_PORT", raw)
		if _, err := LoadDeploymentConfig(); err == nil {
			t.Errorf("WEBUI_PORT=%q 应报错，但通过了校验", raw)
		} else if !strings.Contains(err.Error(), "WEBUI_PORT") {
			t.Errorf("WEBUI_PORT=%q 的错误信息应指出键名，实际: %v", raw, err)
		}
	}
}

// TestLoadDeploymentConfig_BusinessEnvIgnored 业务环境变量不影响部署参数
func TestLoadDeploymentConfig_BusinessEnvIgnored(t *testing.T) {
	t.Setenv("FWALIZER_DATA_DIR", "")
	t.Setenv("WEBUI_HOST", "")
	t.Setenv("WEBUI_PORT", "")
	t.Setenv("TARGETS", "tc_lighthouse|lhins-abc|ap-guangzhou")
	t.Setenv("TC_ACCESS_ID", "AKIDxxxx")
	t.Setenv("INTERVAL", "10m")
	t.Setenv("DNS", "1.1.1.1")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("FWALIZER_MODE", "env")

	cfg, err := LoadDeploymentConfig()
	if err != nil {
		t.Fatalf("业务环境变量不应影响部署参数校验: %v", err)
	}
	if cfg.DataDir != DefaultDataDir() || cfg.Host != "127.0.0.1" || cfg.Port != 60200 {
		t.Errorf("业务环境变量改变了部署参数: %+v", cfg)
	}
}
