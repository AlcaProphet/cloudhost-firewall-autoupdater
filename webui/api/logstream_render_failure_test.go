package api

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestI8R17RenderFailure 使用临时构建覆盖注入 writer，不修改生产文件或引入运行时 hook。
func TestI8R17RenderFailure(t *testing.T) {
	sourcePath, err := filepath.Abs("logstream.go")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "slog.NewTextHandler(&buf,"
	if strings.Count(string(source), anchor) != 1 {
		t.Fatal("渲染 writer 注入点已变更，需复核测试夹具")
	}
	injected := strings.Replace(string(source), anchor, "slog.NewTextHandler(i8r17WriterFor(&buf, r),", 1)
	dir := t.TempDir()
	injectedPath := filepath.Join(dir, "logstream.go")
	if err := os.WriteFile(injectedPath, []byte(injected), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureData, err := os.ReadFile("testdata/logstream_render_failure.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "injected_test.go")
	if err := os.WriteFile(fixture, fixtureData, 0600); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(filepath.Dir(sourcePath), "i8r17_injected_test.go")
	overlay, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{sourcePath: injectedPath, fixturePath: fixture}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	// 仅运行注入夹具；父测试不会在子进程重入。故障夹具仅加入测试构建。
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-json", "-race", "-count=1", "-timeout=60s", "-overlay="+overlayPath, "-run=^TestI8R17Injected", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("故障注入子测试失败: %v\n%s", err, output)
	}
	passes := map[string]bool{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var event struct{ Action, Test string }
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("子测试输出不是合法事件流: %v", err)
		}
		if event.Action == "pass" {
			passes[event.Test] = true
		}
	}
	for _, name := range []string{"TestI8R17InjectedRenderLine", "TestI8R17InjectedNoPublish", "TestI8R17InjectedAttributeFailureIsText"} {
		if !passes[name] {
			t.Fatalf("注入子测试没有实际通过: %s\n%s", name, output)
		}
	}
	t.Log("三个注入子测试均实际通过，未以无测试或Skip充当成功")
}
