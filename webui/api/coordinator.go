package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// ConfigCoordinator 进程内配置变更协调器（Build6 Step 4 骨架、§12.4）。
//
// 职责：把目标、规则、settings、alerts、pause/resume、reset 与配置导入的写入口
// 串行化，在同一个 SQLite 事务内完成变更，并在提交后只触发一次运行时更新，
// 避免“后提交先应用”造成的运行时回退。
//
// 过渡结构声明：本轮的 apply 仍封装既有 reload 机制（run.go 注入的闭包，内部
// 依旧是 SetCredentials → ReloadProviders → Reload → ReloadResolver → 告警
// 重订阅的分次应用）。它**不是** Step 5 要求的完整原子 RuntimeState 发布；
// Step 5 必须改成“预构造候选 + 无失败原子替换”并删除分次 reload。
type ConfigCoordinator struct {
	mu    sync.Mutex
	store *config.Store
	apply func()
}

// NewConfigCoordinator 创建协调器。
//
// apply 在事务提交后同步调用一次，只能是无失败的内存操作；传 nil 表示不需要
// 运行时更新（仅测试或纯写入场景）。
func NewConfigCoordinator(store *config.Store, apply func()) *ConfigCoordinator {
	return &ConfigCoordinator{store: store, apply: apply}
}

// Mutate 在锁内执行一次配置变更：
//
//	加锁 → 开启事务 → 执行变更 → 提交 → 触发一次运行时更新
//
// 变更回调返回错误时不提交、不 apply；提交失败同样不 apply。
// 因此非法输入既不会写库，也不会触发 reload。
func (c *ConfigCoordinator) Mutate(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tx, err := c.store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("开始配置事务失败: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			slog.Error("回滚配置事务失败", "error", rbErr)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交配置事务失败: %w", err)
	}
	committed = true

	// commit 之后只做无失败的内存操作；HTTP 成功响应必须在 apply 完成后写出，
	// 使响应之后开始的新操作必然取得新状态。
	if c.apply != nil {
		c.apply()
	}
	return nil
}
