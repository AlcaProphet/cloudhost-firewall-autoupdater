package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// edgeOneSiteRequest 是站点创建/全量更新的请求体（Build8 Step 3）。
//
// target_ids 使用指针区分「缺失」与「显式空数组」：两者都非法，但错误更明确。
// 动作固定为 ACCEPT，不接受 action 字段；提交会触发未知字段拒绝。
type edgeOneSiteRequest struct {
	ZoneID        string `json:"zone_id"`
	TargetIDs     *[]int `json:"target_ids"`
	Protocol      string `json:"protocol"`
	Ports         string `json:"ports"`
	AddressFamily string `json:"address_family"`
}

// toSite 转换为领域值并执行纯本地校验。
func (req edgeOneSiteRequest) toSite() (config.EdgeOneSite, error) {
	if req.TargetIDs == nil {
		return config.EdgeOneSite{}, &config.ValidationError{
			Field:  "target_ids",
			Reason: "必须显式提供且至少包含一个目标",
		}
	}
	return config.NormalizeEdgeOneSite(config.EdgeOneSite{
		ZoneID:        req.ZoneID,
		TargetIDs:     *req.TargetIDs,
		Protocol:      req.Protocol,
		Ports:         req.Ports,
		AddressFamily: req.AddressFamily,
	})
}

func (d *Deps) handleGetEdgeOneSites(w http.ResponseWriter, r *http.Request) {
	sites, err := d.Store.LoadEdgeOneSites()
	if err != nil {
		writeInternalError(w, "读取 EdgeOne 站点失败", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": sites})
}

func (d *Deps) handleAddEdgeOneSite(w http.ResponseWriter, r *http.Request) {
	var req edgeOneSiteRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	site, err := req.toSite()
	if err != nil {
		writeRequestError(w, err)
		return
	}

	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		exists, eerr := d.Store.EdgeOneSiteExistsTx(ctx, tx, site.ZoneID)
		if eerr != nil {
			return eerr
		}
		if exists {
			return conflict("站点已存在")
		}
		if verr := d.Store.ValidateEdgeOneSiteTargetsTx(ctx, tx, site); verr != nil {
			return verr
		}
		return d.Store.AddEdgeOneSiteTx(ctx, tx, site)
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, site)
}

func (d *Deps) handleUpdateEdgeOneSite(w http.ResponseWriter, r *http.Request) {
	pathZoneID, err := config.NormalizeZoneID(r.PathValue("zone_id"))
	if err != nil {
		writeRequestError(w, err)
		return
	}
	var req edgeOneSiteRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	site, err := req.toSite()
	if err != nil {
		writeRequestError(w, err)
		return
	}
	if site.ZoneID != pathZoneID {
		writeRequestError(w, badRequest("路径 zone_id 与请求体不一致"))
		return
	}

	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		exists, eerr := d.Store.EdgeOneSiteExistsTx(ctx, tx, pathZoneID)
		if eerr != nil {
			return eerr
		}
		if !exists {
			return notFound("站点不存在")
		}
		if verr := d.Store.ValidateEdgeOneSiteTargetsTx(ctx, tx, site); verr != nil {
			return verr
		}
		rows, uerr := d.Store.UpdateEdgeOneSiteTx(ctx, tx, site)
		if uerr != nil {
			return uerr
		}
		if rows == 0 {
			return notFound("站点不存在")
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, site)
}

func (d *Deps) handleDeleteEdgeOneSite(w http.ResponseWriter, r *http.Request) {
	zoneID, err := config.NormalizeZoneID(r.PathValue("zone_id"))
	if err != nil {
		writeRequestError(w, err)
		return
	}
	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		rows, derr := d.Store.DeleteEdgeOneSiteTx(ctx, tx, zoneID)
		if derr != nil {
			return derr
		}
		if rows == 0 {
			return notFound("站点不存在")
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}
