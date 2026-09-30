package dns

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// TestCircuitBreaker_CloneForDomains 检查实际条目，避免缺失 key 与零计数混淆。
func TestCircuitBreaker_CloneForDomains(t *testing.T) {
	cb := NewCircuitBreaker(5)
	// 零值条目模拟旧版本遗留状态，过滤复制不能把它带入新实例。
	cb.failCount = map[string]int{"active.example": 2, "removed.example": 4, "zero.example": 0, "Case.Example": 3}
	for _, tt := range []struct {
		name    string
		domains []string
		want    map[string]int
	}{
		{"保留和裁剪", []string{"active.example", "active.example", "zero.example", "new.example", "Case.Example"}, map[string]int{"active.example": 2, "Case.Example": 3}},
		{"域名原值", []string{"case.example"}, map[string]int{}},
		{"空集合", []string{}, map[string]int{}},
		{"nil集合", nil, map[string]int{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clone := cb.CloneForDomains(tt.domains)
			if !reflect.DeepEqual(clone.failCount, tt.want) || clone.threshold != 5 {
				t.Fatalf("复制结果 = %v / 阈值 %d，期望 %v / 5", clone.failCount, clone.threshold, tt.want)
			}
		})
	}
	clone := cb.CloneForDomains([]string{"active.example"})
	clone.SetThreshold(2)
	clone.RecordSuccess("active.example")
	if cb.threshold != 5 || cb.failCount["active.example"] != 2 {
		t.Fatal("新实例修改污染旧实例")
	}
	cb.RecordFailure("removed.example")
	cb.RecordFailure("active.example")
	if len(clone.failCount) != 0 {
		t.Fatal("旧实例写入恢复了新实例条目")
	}
}

// TestCircuitBreaker_CloneForDomainsConcurrent 过滤复制与计数、阈值更新使用同一锁边界。
func TestCircuitBreaker_CloneForDomainsConcurrent(t *testing.T) {
	cb := NewCircuitBreaker(1000)
	cb.failCount = map[string]int{"active.example": 2, "removed.example": 3, "zero.example": 0}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			cb.RecordFailure("mutating.example")
			if i%10 == 0 {
				cb.RecordSuccess("mutating.example")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			cb.SetThreshold(1000 + i)
			cb.IsOpen("mutating.example")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			clone := cb.CloneForDomains([]string{"active.example", "mutating.example", "zero.example"})
			if clone.failCount["active.example"] != 2 {
				t.Error("并发复制丢失保留域名计数")
			}
			for domain, n := range clone.failCount {
				if n <= 0 || (domain != "active.example" && domain != "mutating.example") {
					t.Errorf("并发复制产生无效条目 %s=%d", domain, n)
				}
			}
		}
	}()
	wg.Wait()
}

// TestCircuitBreaker_SuccessRemovesEntry 区分删除条目和保留零值，并检查失败重新计数。
func TestCircuitBreaker_SuccessRemovesEntry(t *testing.T) {
	cb := NewCircuitBreaker(3)
	cb.RecordFailure("active.example")
	cb.RecordSuccess("active.example")
	if _, exists := cb.failCount["active.example"]; exists {
		t.Fatal("成功后仍保留域名条目")
	}
	cb.RecordFailure("active.example")
	if got := failCountOf(cb, "active.example"); got != 1 {
		t.Fatalf("成功后再次失败计数 = %d，期望 1", got)
	}
	cb.RecordSuccess("active.example")
	for i := 0; i < 10000; i++ {
		cb.RecordSuccess(fmt.Sprintf("success-%d.example", i))
	}
	if got := len(cb.failCount); got != 0 {
		t.Fatalf("纯成功域名残留 %d 个条目", got)
	}
}

// failCountOf 读取某域名的当前失败计数（测试辅助）
func failCountOf(cb *CircuitBreaker, domain string) int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failCount[domain]
}

func TestCircuitBreaker_Trigger(t *testing.T) {
	cb := NewCircuitBreaker(3)

	// 未达阈值，不熔断
	cb.RecordFailure("example.com")
	cb.RecordFailure("example.com")
	if cb.IsOpen("example.com") {
		t.Error("未达阈值不应熔断")
	}

	// 达到阈值，触发熔断
	cb.RecordFailure("example.com")
	if !cb.IsOpen("example.com") {
		t.Error("达到阈值应熔断")
	}

	// 其他域名不受影响
	if cb.IsOpen("other.com") {
		t.Error("其他域名不应熔断")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(2)

	cb.RecordFailure("example.com")
	cb.RecordFailure("example.com")
	if !cb.IsOpen("example.com") {
		t.Error("应已熔断")
	}

	// 成功后解除
	cb.RecordSuccess("example.com")
	if cb.IsOpen("example.com") {
		t.Error("成功后应解除熔断")
	}
}

func TestCircuitBreaker_HalfOpenNoIncrement(t *testing.T) {
	cb := NewCircuitBreaker(3)

	// 触发熔断
	cb.RecordFailure("example.com")
	cb.RecordFailure("example.com")
	cb.RecordFailure("example.com")
	if !cb.IsOpen("example.com") {
		t.Error("应已熔断")
	}

	// 半开探测失败：计数器不应继续增长
	cb.RecordFailure("example.com")
	cb.RecordFailure("example.com")

	cb.mu.Lock()
	count := cb.failCount["example.com"]
	cb.mu.Unlock()

	if count != 3 {
		t.Errorf("熔断后计数应保持为 3，实际为 %d", count)
	}

	// 仍然处于熔断状态
	if !cb.IsOpen("example.com") {
		t.Error("半开探测失败后应维持熔断")
	}
}

// TestCircuitBreaker_SetThresholdKeepsFailures 阈值变更必须保留既有失败计数（Build6 Step 4）
func TestCircuitBreaker_SetThresholdKeepsFailures(t *testing.T) {
	cb := NewCircuitBreaker(5)

	cb.RecordFailure("example.com")
	cb.RecordFailure("example.com")
	if failCountOf(cb, "example.com") != 2 {
		t.Fatalf("失败计数 = %d, want 2", failCountOf(cb, "example.com"))
	}

	// 阈值降低到 3：计数保留为 2，尚未熔断
	cb.SetThreshold(3)
	if failCountOf(cb, "example.com") != 2 {
		t.Errorf("SetThreshold 不应重置计数，实际 %d", failCountOf(cb, "example.com"))
	}
	if cb.IsOpen("example.com") {
		t.Error("计数 2 < 阈值 3，不应熔断")
	}

	// 再失败一次即达新阈值
	cb.RecordFailure("example.com")
	if !cb.IsOpen("example.com") {
		t.Error("计数 3 >= 阈值 3，应熔断")
	}

	// 阈值提高后应重新放行，且计数仍保留
	cb.SetThreshold(10)
	if cb.IsOpen("example.com") {
		t.Error("阈值提高后不应继续熔断")
	}
	if failCountOf(cb, "example.com") != 3 {
		t.Errorf("阈值提高后计数应保留为 3，实际 %d", failCountOf(cb, "example.com"))
	}
}

// TestCircuitBreaker_SetThresholdIgnoresNonPositive 非正数阈值被忽略，避免破坏 IsOpen 语义
func TestCircuitBreaker_SetThresholdIgnoresNonPositive(t *testing.T) {
	cb := NewCircuitBreaker(4)
	cb.SetThreshold(0)
	cb.SetThreshold(-2)

	// 阈值仍为 4：3 次失败不应熔断
	for i := 0; i < 3; i++ {
		cb.RecordFailure("example.com")
	}
	if cb.IsOpen("example.com") {
		t.Error("非正数阈值应被忽略，3 次失败不应熔断")
	}
	cb.RecordFailure("example.com")
	if !cb.IsOpen("example.com") {
		t.Error("第 4 次失败应触发熔断")
	}
}

// TestCircuitBreaker_SetThresholdConcurrent 阈值更新与计数读写并发安全
func TestCircuitBreaker_SetThresholdConcurrent(t *testing.T) {
	cb := NewCircuitBreaker(1000)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				cb.RecordFailure("example.com")
				cb.IsOpen("example.com")
				if j%50 == 0 {
					cb.SetThreshold(1000 - i)
				}
			}
		}(i)
	}
	wg.Wait()

	// 计数上限为最后一次设置的阈值以内的合理值，只要求不 panic/不竞态
	if got := failCountOf(cb, "example.com"); got < 0 {
		t.Errorf("失败计数异常: %d", got)
	}
}
