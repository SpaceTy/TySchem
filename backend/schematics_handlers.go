package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MaxUploadSize caps a single .litematic upload (50 MiB).
const MaxUploadSize = 50 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// registerSchematicRoutes mounts the schematic API onto mux.
// Manual path/method dispatch keeps this working on Go 1.21+.
func registerSchematicRoutes(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("/api/schematics", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleSchematicUpload(w, r, store)
		case http.MethodGet:
			handleSchematicList(w, r, store)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/api/schematics/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/schematics/")
		// Normalize: allow optional trailing slash.
		rest = strings.TrimSuffix(rest, "/")
		if rest == "" {
			http.Redirect(w, r, "/api/schematics", http.StatusMovedPermanently)
			return
		}
		id, action := rest, ""
		if i := strings.Index(rest, "/"); i >= 0 {
			id, action = rest[:i], rest[i+1:]
		}
		switch action {
		case "":
			switch r.Method {
			case http.MethodGet:
				handleSchematicGet(w, r, store, id)
			case http.MethodPut, http.MethodPatch:
				handleSchematicUpdate(w, r, store, id)
			case http.MethodDelete:
				handleSchematicDelete(w, r, store, id)
			default:
				w.Header().Set("Allow", "GET, PUT, PATCH, DELETE")
				writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		case "file", "download":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			handleSchematicFile(w, r, store, id)
		default:
			writeErr(w, http.StatusNotFound, "not found")
		}
	})
}

// POST /api/schematics (multipart/form-data)
// Fields: file (required, *.litematic), name (optional), description (optional).
func handleSchematicUpload(w http.ResponseWriter, r *http.Request, store *Store) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize+10<<20)
	if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
		writeErr(w, http.StatusBadRequest, "failed to parse multipart form (max 50 MiB): "+err.Error())
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing required form field 'file'")
		return
	}
	defer f.Close()

	origName := hdr.Filename
	if origName == "" {
		origName = "schematic.litematic"
	}
	origName = filepath.Base(origName) // avoid directory parts from broken clients
	ext := strings.ToLower(filepath.Ext(origName))
	if ext != ".litematic" && ext != ".litematica" {
		writeErr(w, http.StatusBadRequest, "file must have a .litematic extension")
		return
	}

	// Cap in-memory buffering; spill to disk via multipart already, then read fully up to limit.
	data, err := io.ReadAll(io.LimitReader(f, MaxUploadSize+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "failed to read upload: "+err.Error())
		return
	}
	if int64(len(data)) > MaxUploadSize {
		writeErr(w, http.StatusRequestEntityTooLarge, "file exceeds 50 MiB limit")
		return
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		writeErr(w, http.StatusBadRequest, "file does not look like a litematica schematic (expected gzipped NBT)")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSuffix(origName, filepath.Ext(origName))
	}
	if len(name) > 200 {
		writeErr(w, http.StatusBadRequest, "name must be <= 200 characters")
		return
	}
	description := strings.TrimSpace(r.FormValue("description"))
	if len(description) > 5000 {
		writeErr(w, http.StatusBadRequest, "description must be <= 5000 characters")
		return
	}

	meta, err := store.Create(name, description, origName, data)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store schematic")
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

// GET /api/schematics?q=&name=&description=&from=&to=&sort=&order=&limit=&offset=
// Returns {"items":[...],"total":N,"limit":L,"offset":O} — the metadata index
// the frontend uses to resolve IDs before fetching blobs.
func handleSchematicList(w http.ResponseWriter, r *http.Request, store *Store) {
	q := r.URL.Query()
	f := ListFilter{
		Query:       q.Get("q"),
		Name:        q.Get("name"),
		Description: q.Get("description"),
		Sort:        q.Get("sort"),
		Order:       q.Get("order"),
	}
	if f.Sort == "" {
		f.Sort = "uploadDate"
	}
	switch f.Sort {
	case "uploadDate", "updatedDate", "name", "size":
	default:
		writeErr(w, http.StatusBadRequest, "invalid sort (want uploadDate|updatedDate|name|size)")
		return
	}
	if f.Order == "" {
		f.Order = "desc"
	}
	if f.Order != "asc" && f.Order != "desc" {
		writeErr(w, http.StatusBadRequest, "invalid order (want asc|desc)")
		return
	}
	if s := q.Get("from"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid from (want RFC3339)")
			return
		}
		f.From = &t
	}
	if s := q.Get("to"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid to (want RFC3339)")
			return
		}
		f.To = &t
	}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		f.Limit = n
	} else {
		f.Limit = 50
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

	items, total, err := store.List(f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to list schematics")
		return
	}
	if items == nil {
		items = []SchematicMetadata{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  f.Limit,
		"offset": f.Offset,
	})
}

// GET /api/schematics/{id} — metadata by ID.
func handleSchematicGet(w http.ResponseWriter, _ *http.Request, store *Store, id string) {
	meta, err := store.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "schematic not found")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// GET /api/schematics/{id}/file (alias /download) — raw .litematic bytes.
func handleSchematicFile(w http.ResponseWriter, r *http.Request, store *Store, id string) {
	meta, err := store.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "schematic not found")
		return
	}
	path := store.FilePath(id)
	// Double-check the blob still exists.
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, "schematic file missing")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, meta.FileName))
	http.ServeFile(w, r, path)
}

// PUT/PATCH /api/schematics/{id} — JSON {"name"?,"description"?} updates metadata.
func handleSchematicUpdate(w http.ResponseWriter, r *http.Request, store *Store, id string) {
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Name != nil {
		*body.Name = strings.TrimSpace(*body.Name)
		if *body.Name == "" || len(*body.Name) > 200 {
			writeErr(w, http.StatusBadRequest, "name must be 1-200 characters")
			return
		}
	}
	if body.Description != nil {
		*body.Description = strings.TrimSpace(*body.Description)
		if len(*body.Description) > 5000 {
			writeErr(w, http.StatusBadRequest, "description must be <= 5000 characters")
			return
		}
	}
	if body.Name == nil && body.Description == nil {
		writeErr(w, http.StatusBadRequest, "nothing to update (want name and/or description)")
		return
	}
	meta, err := store.Update(id, body.Name, body.Description)
	if err != nil {
		writeErr(w, http.StatusNotFound, "schematic not found")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// DELETE /api/schematics/{id} — removes blob + metadata.
func handleSchematicDelete(w http.ResponseWriter, _ *http.Request, store *Store, id string) {
	if err := store.Delete(id); err != nil {
		writeErr(w, http.StatusNotFound, "schematic not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
