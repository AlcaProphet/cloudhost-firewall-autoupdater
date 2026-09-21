package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func (d *Deps) handleGetRules(w http.ResponseWriter, r *http.Request) {
	rules, err := d.Store.GetRules()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (d *Deps) handleAddRule(w http.ResponseWriter, r *http.Request) {
	var rule config.DomainRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := d.Store.AddRule(rule); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.notifyReload()
	writeJSON(w, http.StatusCreated, map[string]string{"message": "添加成功"})
}

func (d *Deps) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var n int
	if _, err := fmt.Sscanf(id, "%d", &n); err != nil {
		writeError(w, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	var rule config.DomainRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := d.Store.UpdateRule(n, rule); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

func (d *Deps) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var n int
	if _, err := fmt.Sscanf(id, "%d", &n); err != nil {
		writeError(w, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	if err := d.Store.DeleteRule(n); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}
