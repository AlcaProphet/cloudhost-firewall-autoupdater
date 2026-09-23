package api

import (
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
		writeError(w, http.StatusBadRequest, "同步引擎未启动，请先配置目标和规则")
		return
	}
	if !d.Syncer.Status().Enabled {
		writeError(w, http.StatusConflict, "同步已暂停，请先开启")
		return
	}
	d.Syncer.TriggerSync()
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "同步已触发"})
}

// handleSyncPause 暂停同步：先写 DB 后通知 Syncer（即使通知失败，持久化状态已正确写入）
func (d *Deps) handleSyncPause(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil {
		writeError(w, http.StatusBadRequest, "同步引擎未启动")
		return
	}
	if err := d.Store.SetSetting("sync_enabled", "false"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.Syncer.Pause()
	writeJSON(w, http.StatusOK, map[string]string{"message": "同步已暂停"})
}

// handleSyncResume 恢复同步：先写 DB 后通知 Syncer
func (d *Deps) handleSyncResume(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil {
		writeError(w, http.StatusBadRequest, "同步引擎未启动")
		return
	}
	if err := d.Store.SetSetting("sync_enabled", "true"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.Syncer.Resume()
	writeJSON(w, http.StatusOK, map[string]string{"message": "同步已恢复"})
}

func (d *Deps) handleSyncDryRun(w http.ResponseWriter, r *http.Request) {
	if d.Syncer == nil {
		writeError(w, http.StatusBadRequest, "同步引擎未启动，请先配置目标和规则")
		return
	}
	resp, err := d.Syncer.DryRun()
	if err != nil {
		if errors.Is(err, syncer.ErrDryRunInProgress) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSyncEvents SSE 实时事件推送
func (d *Deps) handleSyncEvents(w http.ResponseWriter, r *http.Request) {
	if d.EventBus == nil {
		writeError(w, http.StatusBadRequest, "事件总线不可用")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
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
	flusher.Flush()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return // channel 已关闭
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// handleClearSyncLogs 清空同步历史记录
func (d *Deps) handleClearSyncLogs(w http.ResponseWriter, r *http.Request) {
	if err := d.Store.ClearSyncLogs(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "历史记录已清空"})
}
