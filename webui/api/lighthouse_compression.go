package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// compressionRequest 是实例级策略压缩的全量保存请求（Build9 Step 1）。
//
// 使用指针区分“字段缺失/null”与合法零值：enabled=false 与
// extra_ipv4_allow=0 都是合法值，只有两个字段都显式出现才允许保存。
type compressionRequest struct {
	Enabled        *bool  `json:"enabled"`
	ExtraIPv4Allow *int64 `json:"extra_ipv4_allow"`
}

// compressionResponse 是实例级设置的读取/保存结果。
type compressionResponse struct {
	CloudType      config.CloudType `json:"cloud_type"`
	Region         string           `json:"region"`
	ResourceID     string           `json:"resource_id"`
	Enabled        bool             `json:"enabled"`
	ExtraIPv4Allow int64            `json:"extra_ipv4_allow"`
	PendingReview  bool             `json:"pending_review"`
	PendingSince   string           `json:"pending_since"`
	// TargetIDs 只用于 GET：展示共享同一物理实例的本地别名。
	TargetIDs []int `json:"target_ids,omitempty"`
}

// findTargetByID 从目标列表定位一个目标；不存在返回 false。
func findTargetByID(targets []config.TargetConfig, id int) (config.TargetConfig, bool) {
	for _, target := range targets {
		if target.ID == id {
			return target, true
		}
	}
	return config.TargetConfig{}, false
}

// lighthouseCompressionForTarget 把目标转换为该物理实例的配置键。
func lighthouseCompressionForTarget(target config.TargetConfig) config.LighthouseCompression {
	return config.LighthouseCompression{
		CloudType:  target.CloudType,
		Region:     target.Region,
		ResourceID: target.ResourceID,
	}
}

func (d *Deps) handleGetLighthouseCompression(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	targets, err := d.Store.GetTargets()
	if err != nil {
		writeInternalError(w, "读取目标失败", err)
		return
	}
	target, ok := findTargetByID(targets, id)
	if !ok {
		writeError(w, http.StatusNotFound, "目标不存在")
		return
	}
	if target.CloudType != config.CloudTCLighthouse {
		writeRequestError(w, badRequest("策略压缩仅支持腾讯云 Lighthouse"))
		return
	}

	key := lighthouseCompressionForTarget(target)
	settings, err := d.Store.LoadLighthouseCompressionSettings()
	if err != nil {
		writeInternalError(w, "读取策略压缩配置失败", err)
		return
	}
	out := compressionResponse{
		CloudType:  target.CloudType,
		Region:     target.Region,
		ResourceID: target.ResourceID,
		TargetIDs:  []int{},
	}
	for _, item := range settings {
		if item.Identity() == key.Identity() {
			out.Enabled = item.Enabled
			out.ExtraIPv4Allow = item.ExtraIPv4Allow
			break
		}
	}
	state, found, err := d.Store.LoadLighthouseCompressionState(key)
	if err != nil {
		writeInternalError(w, "读取策略压缩状态失败", err)
		return
	}
	if found {
		out.PendingReview = state.PendingReview
		out.PendingSince = state.PendingSince
	}
	identity := key.Identity()
	for _, candidate := range targets {
		if config.TargetIdentity(candidate) == identity {
			out.TargetIDs = append(out.TargetIDs, candidate.ID)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *Deps) handlePutLighthouseCompression(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	var req compressionRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	if req.Enabled == nil {
		writeRequestError(w, badRequest("enabled 必须显式提供"))
		return
	}
	if req.ExtraIPv4Allow == nil {
		writeRequestError(w, badRequest("extra_ipv4_allow 必须显式提供"))
		return
	}

	var out compressionResponse
	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		target, found, terr := d.Store.LoadTargetByIDTx(ctx, tx, id)
		if terr != nil {
			return terr
		}
		if !found {
			return notFound("目标不存在")
		}
		if target.CloudType != config.CloudTCLighthouse {
			return badRequest("策略压缩仅支持腾讯云 Lighthouse")
		}
		next, nerr := config.NormalizeLighthouseCompression(config.LighthouseCompression{
			CloudType:      target.CloudType,
			Region:         target.Region,
			ResourceID:     target.ResourceID,
			Enabled:        *req.Enabled,
			ExtraIPv4Allow: *req.ExtraIPv4Allow,
		})
		if nerr != nil {
			return nerr
		}

		previous, hadPrevious, perr := d.Store.LoadLighthouseCompressionSettingTx(ctx, tx, next)
		if perr != nil {
			return perr
		}
		wasEnabled := hadPrevious && previous.Enabled
		transitionToDisabled := wasEnabled && !next.Enabled

		if serr := d.Store.SaveLighthouseCompressionTx(ctx, tx, next); serr != nil {
			return serr
		}
		switch {
		case next.Enabled:
			if derr := d.Store.DeleteLighthouseCompressionStateTx(ctx, tx, next); derr != nil {
				return derr
			}
		case transitionToDisabled:
			if serr := d.Store.MarkLighthouseCompressionDisabledTx(ctx, tx, next); serr != nil {
				return serr
			}
		}

		state, stateFound, serr := d.Store.LoadLighthouseCompressionStateTx(ctx, tx, next)
		if serr != nil {
			return serr
		}
		out = compressionResponse{
			CloudType:      next.CloudType,
			Region:         next.Region,
			ResourceID:     next.ResourceID,
			Enabled:        next.Enabled,
			ExtraIPv4Allow: next.ExtraIPv4Allow,
		}
		if stateFound {
			out.PendingReview = state.PendingReview
			out.PendingSince = state.PendingSince
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
