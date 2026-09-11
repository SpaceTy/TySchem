package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// registerProjectRoutes mounts the project API onto mux.
func registerProjectRoutes(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			user, ok := requireUser(w, r, store)
			if !ok {
				return
			}
			handleProjectCreate(w, r, store, user)
		case http.MethodGet:
			handleProjectList(w, r, store)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
		if rest == "" {
			http.Redirect(w, r, "/api/projects", http.StatusMovedPermanently)
			return
		}
		id, action := rest, ""
		if i := strings.Index(rest, "/"); i >= 0 {
			id, action = rest[:i], rest[i+1:]
		}
		switch {
		case action == "":
			switch r.Method {
			case http.MethodGet:
				handleProjectGet(w, r, store, id)
			case http.MethodPut, http.MethodPatch:
				user, ok := requireUser(w, r, store)
				if !ok {
					return
				}
				handleProjectUpdate(w, r, store, id, user)
			case http.MethodDelete:
				user, ok := requireUser(w, r, store)
				if !ok {
					return
				}
				handleProjectDelete(w, r, store, id, user)
			default:
				w.Header().Set("Allow", "GET, PUT, PATCH, DELETE")
				writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case action == "schematics":
			user, ok := requireUser(w, r, store)
			if !ok {
				return
			}
			switch r.Method {
			case http.MethodPut:
				handleProjectSetSchematics(w, r, store, id, user)
			case http.MethodPost:
				handleProjectAddSchematic(w, r, store, id, user)
			case http.MethodDelete:
				handleProjectRemoveSchematic(w, r, store, id, user, r.URL.Query().Get("schematicId"))
			default:
				w.Header().Set("Allow", "PUT, POST, DELETE")
				writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case strings.HasPrefix(action, "schematics/"):
			if r.Method != http.MethodDelete {
				w.Header().Set("Allow", "DELETE")
				writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			user, ok := requireUser(w, r, store)
			if !ok {
				return
			}
			handleProjectRemoveSchematic(w, r, store, id, user, strings.TrimPrefix(action, "schematics/"))
		default:
			writeErr(w, http.StatusNotFound, "not found")
		}
	})
}

// GET /api/projects?q=&author=&owner=me&sort=&order=&limit=&offset=&page=&pageSize=
func handleProjectList(w http.ResponseWriter, r *http.Request, store *Store) {
	q := r.URL.Query()
	f := ProjectFilter{
		Query:     q.Get("q"),
		OwnerName: q.Get("author"),
		Sort:      q.Get("sort"),
		Order:     q.Get("order"),
	}
	limitStr := q.Get("limit")
	if ps := q.Get("pageSize"); ps != "" {
		limitStr = ps
	}
	if limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		f.Limit = n
	} else {
		f.Limit = 24
	}
	if f.Limit > MaxListLimit {
		f.Limit = MaxListLimit
	}
	if s := q.Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "invalid offset")
			return
		}
		f.Offset = n
	}
	if s := q.Get("page"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "invalid page (want >= 1)")
			return
		}
		if f.Limit > 0 {
			f.Offset = (n - 1) * f.Limit
		} else {
			f.Offset = 0
		}
	}
	if owner := q.Get("owner"); owner != "" {
		if owner != "me" {
			writeErr(w, http.StatusBadRequest, "invalid owner (want me)")
			return
		}
		user, ok := currentUser(r, store)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		f.OwnerID = user.ID
	}
	if f.Sort == "" {
		f.Sort = "createdDate"
	}
	switch f.Sort {
	case "createdDate", "updatedDate", "name", "schematics":
	default:
		writeErr(w, http.StatusBadRequest, "invalid sort (want createdDate|updatedDate|name|schematics)")
		return
	}
	if f.Order == "" {
		f.Order = "desc"
	}
	if f.Order != "asc" && f.Order != "desc" {
		writeErr(w, http.StatusBadRequest, "invalid order (want asc|desc)")
		return
	}
	items, total, err := store.ListProjects(f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	if items == nil {
		items = []Project{}
	}
	page, totalPages := 1, 0
	if f.Limit > 0 {
		page = f.Offset/f.Limit + 1
		totalPages = (total + f.Limit - 1) / f.Limit
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"total":      total,
		"limit":      f.Limit,
		"offset":     f.Offset,
		"page":       page,
		"pageSize":   f.Limit,
		"totalPages": totalPages,
	})
}

// GET /api/projects/{id} — project metadata plus its ordered schematics.
func handleProjectGet(w http.ResponseWriter, r *http.Request, store *Store, id string) {
	viewerID := ""
	if user, ok := currentUser(r, store); ok {
		viewerID = user.ID
	}
	detail, err := store.ProjectDetail(id, viewerID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// POST /api/projects — JSON {"name","description"?}
func handleProjectCreate(w http.ResponseWriter, r *http.Request, store *Store, user User) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	p, err := store.CreateProject(user.ID, body.Name, body.Description)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p.OwnerName = user.Username
	writeJSON(w, http.StatusCreated, p)
}

// PUT/PATCH /api/projects/{id} — JSON {"name"?,"description"?} (owner only).
func handleProjectUpdate(w http.ResponseWriter, r *http.Request, store *Store, id string, user User) {
	p, err := store.GetProject(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !CanManageProject(p, user) {
		writeErr(w, http.StatusForbidden, "you do not own this project")
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Name == nil && body.Description == nil {
		writeErr(w, http.StatusBadRequest, "nothing to update (want name and/or description)")
		return
	}
	updated, err := store.UpdateProject(id, body.Name, body.Description)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project not found")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DELETE /api/projects/{id} — removes the project (owner only).
func handleProjectDelete(w http.ResponseWriter, _ *http.Request, store *Store, id string, user User) {
	p, err := store.GetProject(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !CanManageProject(p, user) {
		writeErr(w, http.StatusForbidden, "you do not own this project")
		return
	}
	if err := store.DeleteProject(id); err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/projects/{id}/schematics — JSON {"schematicIds":[...]} sets order.
func handleProjectSetSchematics(w http.ResponseWriter, r *http.Request, store *Store, id string, user User) {
	p, err := store.GetProject(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !CanManageProject(p, user) {
		writeErr(w, http.StatusForbidden, "you do not own this project")
		return
	}
	var body struct {
		SchematicIDs []string `json:"schematicIds"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	for _, sid := range body.SchematicIDs {
		meta, err := store.Get(sid)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "schematic not found")
			return
		}
		if !CanManage(meta, user) {
			writeErr(w, http.StatusForbidden, "you cannot add one of these schematics")
			return
		}
	}
	if err := store.SetProjectSchematics(id, body.SchematicIDs); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update project schematics")
		return
	}
	writeProjectDetail(w, r, store, id, user)
}

// POST /api/projects/{id}/schematics — JSON {"schematicId":"..."} appends one.
func handleProjectAddSchematic(w http.ResponseWriter, r *http.Request, store *Store, id string, user User) {
	p, err := store.GetProject(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !CanManageProject(p, user) {
		writeErr(w, http.StatusForbidden, "you do not own this project")
		return
	}
	var body struct {
		SchematicID string `json:"schematicId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	meta, err := store.Get(body.SchematicID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "schematic not found")
		return
	}
	if !CanManage(meta, user) {
		writeErr(w, http.StatusForbidden, "you cannot add this schematic")
		return
	}
	if err := store.AddProjectSchematic(id, body.SchematicID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add schematic")
		return
	}
	writeProjectDetail(w, r, store, id, user)
}

// DELETE /api/projects/{id}/schematics/{schematicId} or ?schematicId=
func handleProjectRemoveSchematic(w http.ResponseWriter, r *http.Request, store *Store, id string, user User, schematicID string) {
	if schematicID == "" {
		writeErr(w, http.StatusBadRequest, "missing schematic id")
		return
	}
	p, err := store.GetProject(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !CanManageProject(p, user) {
		writeErr(w, http.StatusForbidden, "you do not own this project")
		return
	}
	if err := store.RemoveProjectSchematic(id, schematicID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to remove schematic")
		return
	}
	writeProjectDetail(w, r, store, id, user)
}

func writeProjectDetail(w http.ResponseWriter, r *http.Request, store *Store, id string, user User) {
	detail, err := store.ProjectDetail(id, user.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
