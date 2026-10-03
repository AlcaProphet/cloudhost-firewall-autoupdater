package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
)

func (d *Deps) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil {
		writeJSON(w, http.StatusOK, syncer.SyncStatus{Running: false})
		return
	}
	writeJSON(w, http.StatusOK, d.Syncer.Status())
}

func (d *Deps) handleSyncTrigger(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "同步引擎未启动")
		return
	}
	if !d.Syncer.Status().Enabled {
		writeError(w, http.StatusConflict, "同步已暂停，请先开启")
		return
	}
	d.Syncer.TriggerSync()
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "同步已触发"})
}

// handleSyncPause 暂停同步：经协调器写入 sync_enabled，commit 后原子发布新运行时状态。
//
// 协调器的 apply 会把 SyncEnabled=false 写进新的 RuntimeState 并通知 Run goroutine，
// 因此「先写 DB 后生效」的语义不变。**不得**在协调器之外再调用 Syncer.Pause()：
// 那是第二次运行时写入，迟到时会覆盖后提交的真值，造成 SQLite 与运行时分裂（Issue6 A5）。
func (d *Deps) handleSyncPause(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "同步引擎未启动")
		return
	}
	err := d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return d.Store.SetSettingTx(ctx, tx, "sync_enabled", "false")
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "同步已暂停"})
}

// handleSyncResume 恢复同步：经协调器写入 sync_enabled，commit 后原子发布新运行时状态。
//
// false → true 的转换由 Run goroutine 消费控制通知后立即触发一轮（Build6 §12.5）。
// 同样不得在协调器之外再调用 Syncer.Resume()（Issue6 A5）。
func (d *Deps) handleSyncResume(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "同步引擎未启动")
		return
	}
	err := d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return d.Store.SetSettingTx(ctx, tx, "sync_enabled", "true")
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "同步已恢复"})
}

func (d *Deps) handleSyncDryRun(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "同步引擎未启动")
		return
	}
	resp, err := d.Syncer.DryRun()
	if err != nil {
		if errors.Is(err, syncer.ErrDryRunInProgress) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeInternalError(w, "模拟测试失败", err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSyncEvents SSE 实时事件推送
func (d *Deps) handleSyncEvents(w http.ResponseWriter, r *http.Request) {
	if d.EventBus == nil {
		writeError(w, http.StatusServiceUnavailable, "事件总线不可用")
		return
	}

	// 能力检测必须在写响应头之前：两类 SSE 都要求 Flush 与单次写 deadline
	// 可用，不能静默降级为可能无限阻塞的连接（Issue6 A15）。
	if err := probeSSE(w); err != nil {
		slog.Warn("同步事件 SSE 能力检测失败", "error", err)
		writeError(w, http.StatusInternalServerError, "SSE 不可用")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := d.EventBus.SubscribeChan()
	defer unsubscribe()

	// 立即写出响应头并建立订阅：客户端 http.Get 在收到头后即可确认“连接已建立”。
	// 不影响任何既有事件推送语义，只让连接建立与订阅建立对调用方可见。
	// 失败时直接返回：响应头已发出，**不得**再写第二个响应头或 500。
	if err := flushSSE(w); err != nil {
		slog.Warn("同步事件 SSE 初始刷新失败，结束连接", "error", err)
		return
	}

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return // channel 已关闭
			}
			data, err := json.Marshal(ev)
			if err != nil {
				// 自定义序列化错误可能含敏感数据，只记录类型，不输出错误原文或负载。
				slog.Warn("同步事件 SSE 序列化失败，跳过事件", "type", ev.Type, "error_type", fmt.Sprintf("%T", err))
				continue
			}
			if err := writeSSE(w, "data: %s\n\n", data); err != nil {
				// 半开连接 / 客户端停止读取：首个写错误即退出（修复前会永久循环）
				slog.Debug("同步事件 SSE 写出失败，结束连接", "error", err)
				return
			}
		case <-r.Context().Done():
			return
		case <-d.ShutdownCh:
			// 服务器级 shutdown：主动返回，由 defer unsubscribe() 取消订阅。
			// 不通过关闭 EventBus 订阅 channel 驱动退出（保持 Step 1 契约）。
			slog.Info("服务器关闭，同步事件 SSE 退出")
			return
		}
	}
}

func (d *Deps) handleGetSyncLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := d.Store.GetSyncLogs(100)
	if err != nil {
		writeInternalError(w, "读取同步日志失败", err)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// handleClearSyncLogs 清空同步历史记录
func (d *Deps) handleClearSyncLogs(w http.ResponseWriter, r *http.Request) {
	if err := d.Store.ClearSyncLogs(); err != nil {
		writeInternalError(w, "清空同步日志失败", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "历史记录已清空"})
}
