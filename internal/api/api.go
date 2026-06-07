package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crashtest_api/internal/db"
)

type Handler struct {
	store  *db.Store
	logger *slog.Logger
}

func New(store *db.Store, logger *slog.Logger) *Handler {
	return &Handler{store: store, logger: logger}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /apis", h.create)
	mux.HandleFunc("GET /apis", h.list)
	mux.HandleFunc("GET /apis/{id}", h.get)
	mux.HandleFunc("PUT /apis/{id}", h.update)
	mux.HandleFunc("DELETE /apis/{id}", h.delete)
	h.registerIntercepts(mux)
}

type apiResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	BaseURL     string    `json:"base_url"`
	Description *string   `json:"description,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toResponse(a *db.API) apiResponse {
	r := apiResponse{
		ID:        a.ID,
		Name:      a.Name,
		BaseURL:   a.BaseURL,
		Enabled:   a.Enabled,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
	if a.Description.Valid {
		s := a.Description.String
		r.Description = &s
	}
	return r
}

type createRequest struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	BaseURL     string  `json:"base_url"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.BaseURL = strings.TrimSpace(req.BaseURL)

	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validateBaseURL(req.BaseURL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	in := db.APIInput{
		ID:          req.ID,
		Name:        req.Name,
		BaseURL:     req.BaseURL,
		Description: nullString(req.Description),
		Enabled:     enabled,
	}

	created, err := h.store.CreateAPI(r.Context(), in)
	if errors.Is(err, db.ErrAPIAlreadyExists) {
		writeError(w, http.StatusConflict, "api with that id already exists")
		return
	}
	if err != nil {
		h.logger.Error("create api", "error", err, "id", req.ID)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	apis, err := h.store.ListAPIs(r.Context())
	if err != nil {
		h.logger.Error("list apis", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]apiResponse, 0, len(apis))
	for i := range apis {
		out = append(out, toResponse(&apis[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apis": out})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := h.store.GetAPIByID(r.Context(), id)
	if errors.Is(err, db.ErrAPINotFound) {
		writeError(w, http.StatusNotFound, "api not found")
		return
	}
	if err != nil {
		h.logger.Error("get api", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(a))
}

type updateRequest struct {
	Name        *string `json:"name"`
	BaseURL     *string `json:"base_url"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
	// distinguishes "omit description" from "set description to null"
	descPresent bool
}

func (u *updateRequest) UnmarshalJSON(data []byte) error {
	type alias updateRequest
	raw := struct {
		Description json.RawMessage `json:"description"`
		*alias
	}{alias: (*alias)(u)}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Description != nil {
		u.descPresent = true
		if string(raw.Description) == "null" {
			u.Description = nil
		} else {
			var s string
			if err := json.Unmarshal(raw.Description, &s); err != nil {
				return err
			}
			u.Description = &s
		}
	}
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	up := db.APIUpdate{Enabled: req.Enabled}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		up.Name = &trimmed
	}
	if req.BaseURL != nil {
		trimmed := strings.TrimSpace(*req.BaseURL)
		if err := validateBaseURL(trimmed); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		up.BaseURL = &trimmed
	}
	if req.descPresent {
		desc := nullString(req.Description)
		up.Description = &desc
	}

	updated, err := h.store.UpdateAPI(r.Context(), id, up)
	if errors.Is(err, db.ErrAPINotFound) {
		writeError(w, http.StatusNotFound, "api not found")
		return
	}
	if err != nil {
		h.logger.Error("update api", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := h.store.DeleteAPI(r.Context(), id)
	if errors.Is(err, db.ErrAPINotFound) {
		writeError(w, http.StatusNotFound, "api not found")
		return
	}
	if err != nil {
		h.logger.Error("delete api", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return errors.New("base_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("base_url is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("base_url must use http or https scheme")
	}
	if u.Host == "" {
		return errors.New("base_url must include a host")
	}
	return nil
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
