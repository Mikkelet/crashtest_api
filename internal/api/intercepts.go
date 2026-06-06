package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mikkelthygesen/api_proxy/internal/db"
)

var allowedMethods = map[string]struct{}{
	"ANY": {}, "GET": {}, "POST": {}, "PUT": {}, "PATCH": {},
	"DELETE": {}, "HEAD": {}, "OPTIONS": {},
}

const (
	maxDelayMS    = 60_000
	minStatusCode = 100
	maxStatusCode = 599
)

func (h *Handler) registerIntercepts(mux *http.ServeMux) {
	mux.HandleFunc("POST /apis/{apiId}/intercepts", h.createIntercept)
	mux.HandleFunc("GET /apis/{apiId}/intercepts", h.listIntercepts)
	mux.HandleFunc("GET /apis/{apiId}/intercepts/{id}", h.getIntercept)
	mux.HandleFunc("PUT /apis/{apiId}/intercepts/{id}", h.updateIntercept)
	mux.HandleFunc("DELETE /apis/{apiId}/intercepts/{id}", h.deleteIntercept)
}

type interceptResponse struct {
	ID              string            `json:"id"`
	APIID           string            `json:"api_id"`
	Name            string            `json:"name"`
	Method          string            `json:"method"`
	PathPattern     string            `json:"path_pattern"`
	Priority        int               `json:"priority"`
	StatusCode      int               `json:"status_code"`
	ResponseHeaders map[string]string `json:"response_headers"`
	ResponseBody    string            `json:"response_body"`
	DelayMS         int               `json:"delay_ms"`
	Enabled         bool              `json:"enabled"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func toInterceptResponse(r *db.InterceptRule) interceptResponse {
	return interceptResponse{
		ID:              r.ID,
		APIID:           r.APIID,
		Name:            r.Name,
		Method:          r.Method,
		PathPattern:     r.PathPattern,
		Priority:        r.Priority,
		StatusCode:      r.StatusCode,
		ResponseHeaders: r.ResponseHeaders,
		ResponseBody:    r.ResponseBody,
		DelayMS:         r.DelayMS,
		Enabled:         r.Enabled,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

type createInterceptRequest struct {
	Name            string            `json:"name"`
	Method          string            `json:"method"`
	PathPattern     string            `json:"path_pattern"`
	Priority        *int              `json:"priority"`
	StatusCode      int               `json:"status_code"`
	ResponseHeaders map[string]string `json:"response_headers"`
	ResponseBody    string            `json:"response_body"`
	DelayMS         *int              `json:"delay_ms"`
	Enabled         *bool             `json:"enabled"`
}

func (h *Handler) createIntercept(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")

	var req createInterceptRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	in, msg := validateInterceptCreate(apiID, req)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	created, err := h.store.CreateInterceptRule(r.Context(), in)
	if errors.Is(err, db.ErrAPINotFound) {
		writeError(w, http.StatusNotFound, "api not found")
		return
	}
	if err != nil {
		h.logger.Error("create intercept", "error", err, "api_id", apiID)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toInterceptResponse(created))
}

func (h *Handler) listIntercepts(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	rules, err := h.store.ListInterceptRules(r.Context(), apiID)
	if err != nil {
		h.logger.Error("list intercepts", "error", err, "api_id", apiID)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]interceptResponse, 0, len(rules))
	for i := range rules {
		out = append(out, toInterceptResponse(&rules[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"intercepts": out})
}

func (h *Handler) getIntercept(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	id := r.PathValue("id")
	rule, err := h.store.GetInterceptRule(r.Context(), apiID, id)
	if errors.Is(err, db.ErrInterceptRuleNotFound) {
		writeError(w, http.StatusNotFound, "intercept rule not found")
		return
	}
	if err != nil {
		h.logger.Error("get intercept", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toInterceptResponse(rule))
}

type updateInterceptRequest struct {
	Name            *string            `json:"name"`
	Method          *string            `json:"method"`
	PathPattern     *string            `json:"path_pattern"`
	Priority        *int               `json:"priority"`
	StatusCode      *int               `json:"status_code"`
	ResponseHeaders *map[string]string `json:"response_headers"`
	ResponseBody    *string            `json:"response_body"`
	DelayMS         *int               `json:"delay_ms"`
	Enabled         *bool              `json:"enabled"`
}

func (h *Handler) updateIntercept(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	id := r.PathValue("id")

	var req updateInterceptRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	up, msg := validateInterceptUpdate(req)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	updated, err := h.store.UpdateInterceptRule(r.Context(), apiID, id, up)
	if errors.Is(err, db.ErrInterceptRuleNotFound) {
		writeError(w, http.StatusNotFound, "intercept rule not found")
		return
	}
	if err != nil {
		h.logger.Error("update intercept", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toInterceptResponse(updated))
}

func (h *Handler) deleteIntercept(w http.ResponseWriter, r *http.Request) {
	apiID := r.PathValue("apiId")
	id := r.PathValue("id")
	err := h.store.DeleteInterceptRule(r.Context(), apiID, id)
	if errors.Is(err, db.ErrInterceptRuleNotFound) {
		writeError(w, http.StatusNotFound, "intercept rule not found")
		return
	}
	if err != nil {
		h.logger.Error("delete intercept", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateInterceptCreate(apiID string, req createInterceptRequest) (db.InterceptRuleInput, string) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return db.InterceptRuleInput{}, "name is required"
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "ANY"
	}
	if _, ok := allowedMethods[method]; !ok {
		return db.InterceptRuleInput{}, "method must be one of ANY, GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS"
	}
	pattern := strings.TrimSpace(req.PathPattern)
	if pattern == "" {
		return db.InterceptRuleInput{}, "path_pattern is required"
	}
	if !strings.HasPrefix(pattern, "/") {
		return db.InterceptRuleInput{}, "path_pattern must start with /"
	}
	if req.StatusCode < minStatusCode || req.StatusCode > maxStatusCode {
		return db.InterceptRuleInput{}, "status_code must be between 100 and 599"
	}
	delay := 0
	if req.DelayMS != nil {
		delay = *req.DelayMS
	}
	if delay < 0 || delay > maxDelayMS {
		return db.InterceptRuleInput{}, "delay_ms must be between 0 and 60000"
	}
	priority := 0
	if req.Priority != nil {
		priority = *req.Priority
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	headers := req.ResponseHeaders
	if headers == nil {
		headers = map[string]string{}
	}

	id, err := newInterceptID()
	if err != nil {
		return db.InterceptRuleInput{}, "failed to allocate id"
	}

	return db.InterceptRuleInput{
		ID:              id,
		APIID:           apiID,
		Name:            name,
		Method:          method,
		PathPattern:     pattern,
		Priority:        priority,
		StatusCode:      req.StatusCode,
		ResponseHeaders: headers,
		ResponseBody:    req.ResponseBody,
		DelayMS:         delay,
		Enabled:         enabled,
	}, ""
}

func validateInterceptUpdate(req updateInterceptRequest) (db.InterceptRuleUpdate, string) {
	up := db.InterceptRuleUpdate{
		Priority: req.Priority,
		DelayMS:  req.DelayMS,
		Enabled:  req.Enabled,
	}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			return up, "name cannot be empty"
		}
		up.Name = &trimmed
	}
	if req.Method != nil {
		m := strings.ToUpper(strings.TrimSpace(*req.Method))
		if _, ok := allowedMethods[m]; !ok {
			return up, "method must be one of ANY, GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS"
		}
		up.Method = &m
	}
	if req.PathPattern != nil {
		p := strings.TrimSpace(*req.PathPattern)
		if p == "" {
			return up, "path_pattern cannot be empty"
		}
		if !strings.HasPrefix(p, "/") {
			return up, "path_pattern must start with /"
		}
		up.PathPattern = &p
	}
	if req.StatusCode != nil {
		if *req.StatusCode < minStatusCode || *req.StatusCode > maxStatusCode {
			return up, "status_code must be between 100 and 599"
		}
		up.StatusCode = req.StatusCode
	}
	if req.DelayMS != nil {
		if *req.DelayMS < 0 || *req.DelayMS > maxDelayMS {
			return up, "delay_ms must be between 0 and 60000"
		}
	}
	if req.ResponseHeaders != nil {
		up.ResponseHeaders = req.ResponseHeaders
	}
	if req.ResponseBody != nil {
		up.ResponseBody = req.ResponseBody
	}
	return up, ""
}

func newInterceptID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
