package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

// ErrRuntimeCandidateUnsupported 候选运行时构造失败且原因属于请求侧（400）。
//
// 例如配置包/请求给出了未注册的云产品类型：这是输入问题，不是服务器故障。
// 真正的内部构建失败（数据库、内存等）不应包装成该错误，而应走 500。
var ErrRuntimeCandidateUnsupported = errors.New("配置包含不受支持的云产品类型")

// ErrInternalBuild 候选运行时构造失败且原因属于内部错误（500）。
var ErrInternalBuild = errors.New("构造运行时状态失败")

// Candidate 是协调器在事务内构造好的、可在 commit 后无失败发布的一整套内存对象。
//
// State 为候选完整运行时状态（syncer 包拥有其结构与不变量），Alerts 为候选
// 告警订阅集合。二者都在事务提交前构造完成。
type Candidate struct {
	State  *syncer.RuntimeState
	Alerts alertSet
}

// ConfigCoordinator 进程内配置变更协调器（Build6 §12.4）。
//
// 职责：把目标、规则、settings、alerts、pause/resume、reset 与 version 2 配置
// 导入的写入口串行化，并在**同一个 SQLite 事务内**完成变更、读取完整业务快照、
// 构造候选运行时状态与候选告警集合；commit 之后只做无失败的内存发布。
//
// 因此不会出现「后提交先应用」的运行时回退，也不会出现「接口报错但数据库已经改变」。
type ConfigCoordinator struct {
	mu    sync.Mutex
	store *config.Store

	// buildCandidate 在事务内构造候选：只允许本地对象与 SDK client，
	// 不得访问云 API、DNS 上游、SMTP、Webhook 或任何外部网络。
	// policy 决定候选状态的 DNS 熔断策略：普通变更保留既有失败计数，
	// 完整导入新建 breaker 并清空计数（Build6 §12.3 第 7 条）。
	buildCandidate func(snapshot *config.BusinessSnapshot, policy syncer.BreakerPolicy) (Candidate, error)

	// apply 在 commit 之后按固定顺序执行无失败发布：
	// 日志级别 → 告警集合 → RuntimeState。**不得返回 error**：
	// commit 之后不存在可失败出口，避免“接口报错但数据库已经改变”（Build6 §3.5）。
	apply func(candidate Candidate)
}

// ErrNoSnapshotLoader 协调器未配置快照/候选构造能力（接线错误）。
var ErrNoSnapshotLoader = errors.New("协调器未配置候选构造能力")

// NewConfigCoordinator 创建协调器。
//
// buildCandidate 与 apply 必须同时提供；仅做纯写入（无运行时）的场景可传 nil，
// 此时 Mutate 只提交事务、不做运行时发布。
func NewConfigCoordinator(
	store *config.Store,
	buildCandidate func(snapshot *config.BusinessSnapshot, policy syncer.BreakerPolicy) (Candidate, error),
	apply func(candidate Candidate),
) *ConfigCoordinator {
	return &ConfigCoordinator{store: store, buildCandidate: buildCandidate, apply: apply}
}

// Mutate 普通配置变更（目标/规则/settings/alerts/pause/resume/reset）：
// 候选状态沿用 BreakerPreserve，保留既有 DNS 熔断失败计数。
func (c *ConfigCoordinator) Mutate(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	return c.mutate(ctx, syncer.BreakerPreserve, fn)
}

// MutateImport 完整配置导入：候选状态使用 BreakerReset，新建 breaker 并清空原失败计数
// （Build6 §12.3 第 7 条、Issue6 A8）。导入是「整库替换」语义，熔断状态随之重置。
func (c *ConfigCoordinator) MutateImport(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	return c.mutate(ctx, syncer.BreakerReset, fn)
}

// mutate 在锁内执行一次配置变更（Build6 §12.4 固定顺序）：
//
//	加协调器锁 → 开启写事务 → 执行 mutation → 同一事务内读取完整业务快照
//	→ 构造候选 RuntimeState 与候选告警集合 → 提交事务
//	→ 日志级别 → 告警集合 → 发布 RuntimeState → 返回成功
//
// mutation 或候选构造任一失败都完整回滚：数据库、旧 RuntimeState、旧日志级别、
// 旧告警订阅与扫描缓存全部保持原样，且不产生部分 ID 映射。
func (c *ConfigCoordinator) mutate(ctx context.Context, policy syncer.BreakerPolicy, fn func(ctx context.Context, tx *sql.Tx) error) error {
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

	// 事务内读取完整业务快照：导出、导入、候选构造共用同一读取路径，
	// commit 之后不再重新读库（Build6 §12.4、§12.12）。
	snapshot, err := c.store.LoadBusinessSnapshotTx(ctx, tx)
	if err != nil {
		return fmt.Errorf("读取业务快照失败: %w", err)
	}

	var candidate Candidate
	if c.buildCandidate != nil {
		candidate, err = c.buildCandidate(snapshot, policy)
		if err != nil {
			return err
		}
	} else if c.apply != nil {
		// 配置了运行时发布却没有候选构造能力属于接线错误
		return ErrNoSnapshotLoader
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交配置事务失败: %w", err)
	}
	committed = true

	// 以下步骤必须是无 error 的内存操作；HTTP 成功响应必须在 apply 完成后写出，
	// 使响应之后开始的新操作必然取得新状态。
	if c.apply != nil {
		c.apply(candidate)
	}
	return nil
}
