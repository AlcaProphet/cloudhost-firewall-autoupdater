package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func (d *Deps) handleGetRules(w http.ResponseWriter, r *http.Request) {
	rules, err := d.Store.GetRules()
	if err != nil {
		writeInternalError(w, "读取规则失败", err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

// ruleRequest 规则请求体（Build6 §12.9：独立 DTO，不含规则数据库 ID）
//
// Targets 使用指针以区分「字段缺失」与「显式空数组」：空数组表示“适用于全部目标”，
// 而缺失字段若按零值处理会把限定目标的规则静默扩大为全部目标（AGENTS §9.1、Build6 §4.3）。
type ruleRequest struct {
	Host       string `json:"host"`
	Protocol   string `json:"protocol"`
	Ports      string `json:"ports"`
	Action     string `json:"action"`
	Targets    *[]int `json:"targets"`
	Comment    string `json:"comment"`
	EnableIPv6 bool   `json:"enable_ipv6"`
}

// toRule 转换为领域值：先做引用数组的正数/去重校验，再做规则本身归一化。
// 目标是否真实存在依赖数据库，必须在事务内检查。
func (req ruleRequest) toRule() (config.DomainRule, error) {
	if req.Targets == nil {
		return config.DomainRule{}, &config.ValidationError{
			Field:  "targets",
			Reason: "必须显式提供（空数组表示适用于全部目标）",
		}
	}
	targets, err := config.NormalizeTargetIDs(*req.Targets)
	if err != nil {
		return config.DomainRule{}, err
	}
	return config.NormalizeRule(config.DomainRule{
		Host:       req.Host,
		Protocol:   req.Protocol,
		Ports:      req.Ports,
		Action:     req.Action,
		Targets:    targets,
		Comment:    req.Comment,
		EnableIPv6: req.EnableIPv6,
	})
}

func (d *Deps) handleAddRule(w http.ResponseWriter, r *http.Request) {
	var req ruleRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	rule, err := req.toRule()
	if err != nil {
		writeRequestError(w, err)
		return
	}

	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		// 空数组表示“适用于全部目标”；非空时必须在同一事务内确认目标存在
		if verr := d.Store.ValidateRuleTargetsTx(ctx, tx, rule.Targets); verr != nil {
			return verr
		}
		return d.Store.AddRuleTx(ctx, tx, rule)
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"message": "添加成功"})
}

func (d *Deps) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	var req ruleRequest
	if err := decodeJSONStrict(w, r, maxJSONBodyBytes, &req); err != nil {
		writeRequestError(w, err)
		return
	}
	rule, err := req.toRule()
	if err != nil {
		writeRequestError(w, err)
		return
	}

	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if verr := d.Store.ValidateRuleTargetsTx(ctx, tx, rule.Targets); verr != nil {
			return verr
		}
		// 不存在的规则按 RowsAffected=0 判定 404（Build6 §12.7、AGENTS §9.1）
		rows, uerr := d.Store.UpdateRuleTx(ctx, tx, id, rule)
		if uerr != nil {
			return uerr
		}
		if rows == 0 {
			return notFound("规则不存在")
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

func (d *Deps) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	err = d.coordinator().Mutate(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		// 不存在的规则按 RowsAffected=0 判定 404（Build6 §12.7、AGENTS §9.1）
		rows, derr := d.Store.DeleteRuleTx(ctx, tx, id)
		if derr != nil {
			return derr
		}
		if rows == 0 {
			return notFound("规则不存在")
		}
		return nil
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}
