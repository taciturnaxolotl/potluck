package web

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/taciturnaxolotl/potluck/internal/store"
)

// requireAdmin is a middleware that rejects non-admin requests.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := currentUser(r)
		if !ok || u.IsAdmin == 0 {
			writeErr(w, http.StatusForbidden, "forbidden", "admin required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mountAdmin registers /admin/* routes under the admin middleware group.
func (s *Server) mountAdmin(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin)
		r.Get("/admin/users", s.handleAdminListUsers)
		r.Patch("/admin/users/{id}/status", s.handleAdminSetUserStatus)
		r.Patch("/admin/users/{id}/admin", s.handleAdminSetUserAdmin)
		r.Delete("/admin/users/{id}", s.handleAdminDeleteUser)
	})
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Q.ListAllUsers(r.Context())
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{
			"id":           u.ID,
			"email":        u.Email,
			"display_name": u.DisplayName,
			"status":       u.Status,
			"is_admin":     u.IsAdmin == 1,
			"created_at":   u.CreatedAt,
			"last_seen_at": u.LastSeenAt.Int64,
		})
	}
	writeJSON(w, 200, out)
}

type setStatusReq struct {
	Status string `json:"status"`
}

func (s *Server) handleAdminSetUserStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req setStatusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	switch req.Status {
	case "active", "waitlisted", "banned":
	default:
		writeErr(w, 400, "invalid_request", "status must be active, waitlisted, or banned")
		return
	}
	if err := s.Q.SetUserStatus(r.Context(), store.SetUserStatusParams{
		Status: req.Status,
		ID:     id,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	// Revoke all sessions immediately when banning.
	if req.Status == "banned" {
		_ = s.Q.DeleteAllSessionsForUser(r.Context(), id)
	}
	w.WriteHeader(204)
}

type setAdminReq struct {
	IsAdmin bool `json:"is_admin"`
}

func (s *Server) handleAdminSetUserAdmin(w http.ResponseWriter, r *http.Request) {
	actor, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	if id == actor.ID {
		writeErr(w, 400, "invalid_request", "cannot change your own admin status")
		return
	}
	var req setAdminReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid_request", err.Error())
		return
	}
	v := int64(0)
	if req.IsAdmin {
		v = 1
	}
	if err := s.Q.SetUserAdmin(r.Context(), store.SetUserAdminParams{
		IsAdmin: v,
		ID:      id,
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := currentUser(r)
	id := chi.URLParam(r, "id")
	if id == actor.ID {
		writeErr(w, 400, "invalid_request", "cannot delete yourself")
		return
	}
	if err := s.Q.DeleteUserByID(r.Context(), id); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(204)
}
