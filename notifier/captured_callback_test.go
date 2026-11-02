package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 仅在临时构建中暂停真实 Publish；不向产品添加运行时接缝。
func TestI8R19CapturedCallback(t *testing.T) {
	path, err := filepath.Abs("bus.go")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	anchor := "b.mu.RUnlock()\n\n\t// 接口订阅者：锁外异步回调"
	if strings.Count(string(source), anchor) != 1 {
		t.Fatal("I8R19: Publish 捕获与启动位置已变更，需复核夹具")
	}
	injected := strings.Replace(string(source), anchor, "b.mu.RUnlock()\n\tif len(subs) > 0 { i8r19BeforeLaunch() }\n\n\t// 接口订阅者：锁外异步回调", 1)
	injected += "\nvar i8r19BeforeLaunch func()\n"
	// 直接读取夹具，使其成为父测试缓存输入。
	fixture, err := os.ReadFile("testdata/captured_callback.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sourcePath, fixturePath := filepath.Join(dir, "bus.go"), filepath.Join(dir, "injected_test.go")
	if err := os.WriteFile(sourcePath, []byte(injected), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{
		path: sourcePath, filepath.Join(filepath.Dir(path), "i8r19_injected_test.go"): fixturePath,
	}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-json", "-race", "-count=1", "-timeout=40s", "-overlay="+overlayPath, "-run=^TestI8R19InjectedCapturedCallback$", ".")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("I8R19: 注入子测试失败: %v\n%s", err, output)
	}
	runs, passes, packages := 0, 0, 0
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var event struct{ Action, Test string }
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("I8R19: 非法子测试事件流: %v\n%s", err, output)
		}
		if event.Action == "skip" || event.Action == "fail" {
			t.Fatalf("I8R19: 子测试未完整通过\n%s", output)
		}
		if event.Test == "TestI8R19InjectedCapturedCallback" {
			if event.Action == "run" {
				runs++
			}
			if event.Action == "pass" {
				passes++
			}
		}
		if event.Test == "" && event.Action == "pass" {
			packages++
		}
	}
	if runs != 1 || passes != 1 || packages != 1 {
		t.Fatalf("I8R19: 子测试未实际运行并通过一次: run=%d pass=%d package=%d", runs, passes, packages)
	}
}
