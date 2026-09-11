package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SchematicMetadata describes a stored litematica file.
// The ID is the primary key; the frontend lists/filters by metadata,
// then fetches individual files via GET /api/schematics/{id}/file.
type SchematicMetadata struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	FileName    string    `json:"fileName"` // original upload filename
	Size        int64     `json:"size"`     // bytes on disk
	ContentType string    `json:"contentType"`
	OwnerID     string    `json:"ownerId,omitempty"`   // empty for legacy/anonymous uploads
	OwnerName   string    `json:"ownerName,omitempty"` // joined from users
	UploadDate  time.Time `json:"uploadDate"`
	UpdatedDate time.Time `json:"updatedDate"`
	// Aggregate feedback. UserRating/Liked are filled for the viewer when a
	// viewer ID is supplied (0 / false otherwise).
	AvgRating   float64 `json:"avgRating"`
	RatingCount int     `json:"ratingCount"`
	LikeCount   int     `json:"likeCount"`
	UserRating  int     `json:"userRating"`
	Liked       bool    `json:"liked"`
	// Projects this schematic belongs to, filled for detail lookups only.
	Projects []Project `json:"projects,omitempty"`
}

// ListFilter selects which IDs/metadata to return for GET /api/schematics.
type ListFilter struct {
	Query       string     // substring match against name + description + fileName
	Name        string     // substring match against name
	Description string     // substring match against description
	OwnerID     string     // exact owner match (used for "my schematics")
	OwnerName   string     // substring match against owner username
	From        *time.Time // uploadDate >= From
	To          *time.Time // uploadDate <= To
	Sort        string     // "uploadDate" (default) | "updatedDate" | "name" | "size" | "rating" | "likes"
	Order       string     // "asc" | "desc" (default)
	Limit       int        // <=0 means no limit (capped by MaxListLimit)
	Offset      int        // >=0
	ViewerID    string     // fills per-viewer UserRating/Liked when set
}

const MaxListLimit = 200

const schema = `
CREATE TABLE IF NOT EXISTS schematics (
	id           TEXT PRIMARY KEY,
	name         TEXT NOT NULL,
	description  TEXT NOT NULL DEFAULT '',
	file_name    TEXT NOT NULL,
	size         INTEGER NOT NULL,
	content_type TEXT NOT NULL,
	owner_id     TEXT,
	upload_date  TEXT NOT NULL,
	updated_date TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_schematics_name ON schematics(name);
CREATE INDEX IF NOT EXISTS idx_schematics_upload_date ON schematics(upload_date);
CREATE TABLE IF NOT EXISTS users (
	id            TEXT PRIMARY KEY,
	username      TEXT NOT NULL COLLATE NOCASE UNIQUE,
	password_hash TEXT NOT NULL,
	bio           TEXT NOT NULL DEFAULT '',
	is_admin      INTEGER NOT NULL DEFAULT 0,
	created_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS ratings (
	schematic_id TEXT NOT NULL,
	user_id      TEXT NOT NULL,
	rating       INTEGER NOT NULL,
	created_at   TEXT NOT NULL,
	updated_at   TEXT NOT NULL,
	PRIMARY KEY (schematic_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_ratings_schematic ON ratings(schematic_id);
CREATE TABLE IF NOT EXISTS likes (
	schematic_id TEXT NOT NULL,
	user_id      TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	PRIMARY KEY (schematic_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_likes_schematic ON likes(schematic_id);
CREATE TABLE IF NOT EXISTS projects (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	owner_id    TEXT,
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_projects_owner ON projects(owner_id);
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
CREATE TABLE IF NOT EXISTS project_schematics (
	project_id   TEXT NOT NULL,
	schematic_id TEXT NOT NULL,
	position     INTEGER NOT NULL DEFAULT 0,
	added_at     TEXT NOT NULL,
	PRIMARY KEY (project_id, schematic_id)
);
CREATE INDEX IF NOT EXISTS idx_project_schematics_project ON project_schematics(project_id, position);
CREATE INDEX IF NOT EXISTS idx_project_schematics_schematic ON project_schematics(schematic_id);
`

// Store persists schematic blobs on disk and their metadata in SQLite:
//
//	<dataDir>/files/<id>.litematic   (blobs)
//	<dataDir>/schematics.db          (metadata index)
//
// Listing / filtering goes through the SQLite metadata index,
// so the frontend can resolve IDs first and then fetch blobs individually.
type Store struct {
	db       *sql.DB
	filesDir string
}

func NewStore(dataDir string) (*Store, error) {
	filesDir := filepath.Join(dataDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "schematics.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	// Single connection serializes access; plenty for this scale and
	// avoids SQLITE_BUSY under concurrent writers.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite %q: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init sqlite schema: %w", err)
	}
	s := &Store{db: db, filesDir: filesDir}
	// Migration: databases created before accounts existed lack owner_id.
	if err := s.ensureColumn("schematics", "owner_id", "TEXT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schematics.owner_id: %w", err)
	}
	// Index creation must come after the column exists.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_schematics_owner ON schematics(owner_id)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("index schematics.owner_id: %w", err)
	}
	// Migration: databases created before profiles existed lack users.bio.
	if err := s.ensureColumn("users", "bio", "TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate users.bio: %w", err)
	}
	// Migration: databases created before admin accounts existed lack users.is_admin.
	if err := s.ensureColumn("users", "is_admin", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate users.is_admin: %w", err)
	}
	return s, nil
}

// ensureColumn adds a column to a table if it does not already exist.
// table/column names are internal constants, never user input.
func (s *Store) ensureColumn(table, column, decl string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, colType    string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + decl)
	return err
}

// Close releases the SQLite connection. Call on shutdown.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) FilePath(id string) string {
	return filepath.Join(s.filesDir, id+".litematic")
}

var ErrNotFound = errors.New("schematic not found")

// CanManage reports whether user u may edit or delete meta.
// Admins can manage anything; legacy uploads with no owner are manageable by
// any authenticated user.
func CanManage(meta SchematicMetadata, u User) bool {
	return u.IsAdmin || meta.OwnerID == "" || meta.OwnerID == u.ID
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: timestamp-based hex (practically unreachable).
		return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "") + "00"
	}
	// RFC 4122 v4
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

func scanMetadata(row interface {
	Scan(dest ...any) error
}) (SchematicMetadata, error) {
	var m SchematicMetadata
	var uploadDate, updatedDate string
	var ownerID, ownerName sql.NullString
	var liked int
	if err := row.Scan(
		&m.ID, &m.Name, &m.Description, &m.FileName,
		&m.Size, &m.ContentType, &uploadDate, &updatedDate,
		&ownerID, &ownerName,
		&m.AvgRating, &m.RatingCount, &m.LikeCount, &m.UserRating, &liked,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SchematicMetadata{}, ErrNotFound
		}
		return SchematicMetadata{}, err
	}
	m.OwnerID = ownerID.String
	m.OwnerName = ownerName.String
	m.Liked = liked != 0
	var err error
	if m.UploadDate, err = parseTime(uploadDate); err != nil {
		return SchematicMetadata{}, err
	}
	if m.UpdatedDate, err = parseTime(updatedDate); err != nil {
		return SchematicMetadata{}, err
	}
	return m, nil
}

// metaColumns returns the shared SELECT list. viewerID is bound twice (once
// for the viewer's rating, once for their like) and must be passed first in
// the query args, before any WHERE args, because the subqueries precede WHERE.
func metaColumns(viewerID string) string {
	return "s.id, s.name, s.description, s.file_name, s.size, s.content_type, s.upload_date, s.updated_date, s.owner_id, u.username, " +
		`COALESCE((SELECT AVG(r.rating) FROM ratings r WHERE r.schematic_id = s.id), 0), ` +
		`COALESCE((SELECT COUNT(*) FROM ratings r WHERE r.schematic_id = s.id), 0), ` +
		`COALESCE((SELECT COUNT(*) FROM likes l WHERE l.schematic_id = s.id), 0), ` +
		`COALESCE((SELECT r.rating FROM ratings r WHERE r.schematic_id = s.id AND r.user_id = ?), 0), ` +
		`COALESCE((SELECT 1 FROM likes l WHERE l.schematic_id = s.id AND l.user_id = ?), 0)`
}

const metaFrom = "FROM schematics s LEFT JOIN users u ON u.id = s.owner_id"

// Create stores a new schematic blob + metadata row owned by ownerID.
func (s *Store) Create(ownerID, name, description, fileName string, data []byte) (SchematicMetadata, error) {
	id := newID()
	now := time.Now().UTC()
	meta := SchematicMetadata{
		ID:          id,
		Name:        name,
		Description: description,
		FileName:    fileName,
		Size:        int64(len(data)),
		ContentType: "application/octet-stream",
		OwnerID:     ownerID,
		UploadDate:  now,
		UpdatedDate: now,
	}

	if err := os.WriteFile(s.FilePath(id), data, 0o644); err != nil {
		return SchematicMetadata{}, err
	}
	_, err := s.db.Exec(
		`INSERT INTO schematics (id, name, description, file_name, size, content_type, owner_id, upload_date, updated_date)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		meta.ID, meta.Name, meta.Description, meta.FileName,
		meta.Size, meta.ContentType, nullIfEmpty(meta.OwnerID),
		formatTime(meta.UploadDate), formatTime(meta.UpdatedDate),
	)
	if err != nil {
		os.Remove(s.FilePath(id))
		return SchematicMetadata{}, err
	}
	return meta, nil
}

// Get returns metadata for one ID without viewer-specific feedback fields.
func (s *Store) Get(id string) (SchematicMetadata, error) {
	return s.GetViewer(id, "")
}

// GetViewer is Get plus the viewer's own rating and like state.
func (s *Store) GetViewer(id, viewerID string) (SchematicMetadata, error) {
	if !validID(id) {
		return SchematicMetadata{}, ErrNotFound
	}
	meta, err := scanMetadata(s.db.QueryRow(
		`SELECT `+metaColumns(viewerID)+` `+metaFrom+` WHERE s.id = ?`, viewerID, viewerID, id,
	))
	if err != nil {
		return SchematicMetadata{}, err
	}
	// Treat a row with a missing blob as not found (same as before).
	if _, err := os.Stat(s.FilePath(id)); err != nil {
		if os.IsNotExist(err) {
			return SchematicMetadata{}, ErrNotFound
		}
		return SchematicMetadata{}, err
	}
	projects, err := s.ProjectsForSchematic(id)
	if err != nil {
		return SchematicMetadata{}, err
	}
	meta.Projects = projects
	return meta, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Update changes name and/or description. Pass nil for fields to leave untouched.
func (s *Store) Update(id string, name, description *string) (SchematicMetadata, error) {
	if !validID(id) {
		return SchematicMetadata{}, ErrNotFound
	}
	now := formatTime(time.Now().UTC())
	var res sql.Result
	var err error
	switch {
	case name != nil && description != nil:
		res, err = s.db.Exec(`UPDATE schematics SET name = ?, description = ?, updated_date = ? WHERE id = ?`,
			*name, *description, now, id)
	case name != nil:
		res, err = s.db.Exec(`UPDATE schematics SET name = ?, updated_date = ? WHERE id = ?`,
			*name, now, id)
	default: // description != nil (caller guarantees at least one)
		res, err = s.db.Exec(`UPDATE schematics SET description = ?, updated_date = ? WHERE id = ?`,
			*description, now, id)
	}
	if err != nil {
		return SchematicMetadata{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return SchematicMetadata{}, err
	}
	if n == 0 {
		return SchematicMetadata{}, ErrNotFound
	}
	return s.Get(id)
}

// Delete removes both blob and metadata row. Missing ID => ErrNotFound.
func (s *Store) Delete(id string) error {
	if !validID(id) {
		return ErrNotFound
	}
	// Remove blob first; keep going even if it is already gone.
	if err := os.Remove(s.FilePath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM project_schematics WHERE schematic_id = ?`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM schematics WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// escapeLike escapes %, _ and the escape char for a LIKE pattern.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// whereClause builds the shared WHERE clause + args for List's
// COUNT and SELECT queries. Substring matches are case-insensitive.
func whereClause(f ListFilter) (string, []any) {
	var conds []string
	var args []any
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + escapeLike(strings.ToLower(q)) + "%"
		conds = append(conds, `(LOWER(s.name) LIKE ? ESCAPE '\' OR LOWER(s.description) LIKE ? ESCAPE '\' OR LOWER(s.file_name) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	if n := strings.TrimSpace(f.Name); n != "" {
		like := "%" + escapeLike(strings.ToLower(n)) + "%"
		conds = append(conds, `LOWER(s.name) LIKE ? ESCAPE '\'`)
		args = append(args, like)
	}
	if d := strings.TrimSpace(f.Description); d != "" {
		like := "%" + escapeLike(strings.ToLower(d)) + "%"
		conds = append(conds, `LOWER(s.description) LIKE ? ESCAPE '\'`)
		args = append(args, like)
	}
	if f.OwnerID != "" {
		conds = append(conds, `s.owner_id = ?`)
		args = append(args, f.OwnerID)
	}
	if o := strings.TrimSpace(f.OwnerName); o != "" {
		like := "%" + escapeLike(strings.ToLower(o)) + "%"
		conds = append(conds, `LOWER(u.username) LIKE ? ESCAPE '\'`)
		args = append(args, like)
	}
	if f.From != nil {
		conds = append(conds, `s.upload_date >= ?`)
		args = append(args, formatTime(*f.From))
	}
	if f.To != nil {
		conds = append(conds, `s.upload_date <= ?`)
		args = append(args, formatTime(*f.To))
	}
	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// List queries the SQLite metadata index and applies filter/sort/pagination.
// Returns the page plus the total number of matches (before pagination).
func (s *Store) List(f ListFilter) ([]SchematicMetadata, int, error) {
	where, args := whereClause(f)

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) `+metaFrom+` `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Whitelisted sort column (name sorts case-insensitively, as before).
	var orderBy string
	switch f.Sort {
	case "name":
		orderBy = "s.name COLLATE NOCASE"
	case "size":
		orderBy = "s.size"
	case "updatedDate":
		orderBy = "s.updated_date"
	case "rating":
		orderBy = `COALESCE((SELECT AVG(r.rating) FROM ratings r WHERE r.schematic_id = s.id), 0)`
	case "likes":
		orderBy = `COALESCE((SELECT COUNT(*) FROM likes l WHERE l.schematic_id = s.id), 0)`
	default: // "uploadDate"
		orderBy = "s.upload_date"
	}
	order := "DESC"
	if strings.EqualFold(f.Order, "asc") {
		order = "ASC"
	}
	// Tie-break on ID for stable pagination.
	query := `SELECT ` + metaColumns(f.ViewerID) + ` ` + metaFrom + ` ` + where +
		` ORDER BY ` + orderBy + ` ` + order + `, s.id ASC`

	// metaColumns binds viewerID twice before the WHERE args.
	queryArgs := append([]any{f.ViewerID, f.ViewerID}, args...)
	if f.Limit > 0 {
		limit := f.Limit
		if limit > MaxListLimit {
			limit = MaxListLimit
		}
		query += ` LIMIT ?`
		queryArgs = append(queryArgs, limit)
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > 0 {
		if f.Limit <= 0 {
			query += ` LIMIT -1`
		}
		query += ` OFFSET ?`
		queryArgs = append(queryArgs, offset)
	}

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []SchematicMetadata{}
	for rows.Next() {
		m, err := scanMetadata(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// SetRating inserts or updates a user's 1-5 rating for a schematic.
func (s *Store) SetRating(schematicID, userID string, rating int) error {
	if rating < 1 || rating > 5 {
		return errors.New("rating must be between 1 and 5")
	}
	now := formatTime(time.Now().UTC())
	_, err := s.db.Exec(`
		INSERT INTO ratings (schematic_id, user_id, rating, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(schematic_id, user_id)
		DO UPDATE SET rating = excluded.rating, updated_at = excluded.updated_at`,
		schematicID, userID, rating, now, now)
	return err
}

// ClearRating removes a user's rating for a schematic.
func (s *Store) ClearRating(schematicID, userID string) error {
	_, err := s.db.Exec(`DELETE FROM ratings WHERE schematic_id = ? AND user_id = ?`, schematicID, userID)
	return err
}

// ToggleLike flips a user's like on a schematic and reports the new state.
func (s *Store) ToggleLike(schematicID, userID string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM likes WHERE schematic_id = ? AND user_id = ?`, schematicID, userID,
	).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = s.db.Exec(
			`INSERT INTO likes (schematic_id, user_id, created_at) VALUES (?, ?, ?)`,
			schematicID, userID, formatTime(time.Now().UTC()),
		)
		return true, err
	case err != nil:
		return false, err
	default:
		_, err = s.db.Exec(`DELETE FROM likes WHERE schematic_id = ? AND user_id = ?`, schematicID, userID)
		return false, err
	}
}
