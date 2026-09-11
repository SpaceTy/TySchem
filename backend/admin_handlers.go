package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// requireAdmin writes a 403 and returns false unless the request is signed in
// with an administrator account.
func requireAdmin(w http.ResponseWriter, r *http.Request, store *Store) (User, bool) {
	u, ok := requireUser(w, r, store)
	if !ok {
		return User{}, false
	}
	if !u.IsAdmin {
		writeErr(w, http.StatusForbidden, "administrator access required")
		return User{}, false
	}
	return u, true
}

// registerAdminRoutes mounts the administrator API (see AGENTS.md).
func registerAdminRoutes(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("/api/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if _, ok := requireAdmin(w, r, store); !ok {
			return
		}
		users, err := store.ListUsers()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": users})
	})

	mux.HandleFunc("/api/admin/users/", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := requireAdmin(w, r, store)
		if !ok {
			return
		}
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/users/"), "/")
		if !validID(id) {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
		switch r.Method {
		case http.MethodDelete:
			handleAdminDeleteUser(w, store, admin, id)
		case http.MethodPut, http.MethodPatch:
			handleAdminUpdateUser(w, r, store, admin, id)
		default:
			w.Header().Set("Allow", "DELETE, PUT, PATCH")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

// DELETE /api/admin/users/{id} — removes the account and everything it owns.
func handleAdminDeleteUser(w http.ResponseWriter, store *Store, admin User, id string) {
	if id == admin.ID {
		writeErr(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	if err := store.DeleteUser(id); err != nil {
		if errors.Is(err, ErrUnauthorized) || errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/admin/users/{id} — JSON {"isAdmin"?:bool,"password"?:string}.
func handleAdminUpdateUser(w http.ResponseWriter, r *http.Request, store *Store, admin User, id string) {
	var body struct {
		IsAdmin  *bool   `json:"isAdmin"`
		Password *string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.IsAdmin == nil && body.Password == nil {
		writeErr(w, http.StatusBadRequest, "nothing to update (want isAdmin and/or password)")
		return
	}
	if body.IsAdmin != nil {
		if id == admin.ID && !*body.IsAdmin {
			writeErr(w, http.StatusBadRequest, "cannot revoke your own admin access")
			return
		}
		if err := store.SetAdmin(id, *body.IsAdmin); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to update user")
			return
		}
	}
	if body.Password != nil {
		if err := store.SetPassword(id, *body.Password); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	u, err := store.GetUser(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}
