package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Project groups an ordered set of schematics. Membership is many-to-many:
// a schematic may appear in multiple projects, each with its own position.
type Project struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	OwnerID        string    `json:"ownerId,omitempty"`
	OwnerName      string    `json:"ownerName,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	SchematicCount int       `json:"schematicCount"`
}

// ProjectDetail is a project plus its schematics in display order.
type ProjectDetail struct {
	Project    Project             `json:"project"`
	Schematics []SchematicMetadata `json:"schematics"`
}

// ProjectFilter selects which projects to return for GET /api/projects.
type ProjectFilter struct {
	Query     string // substring match against name + description
	OwnerID   string // exact owner match (used for "my projects")
	OwnerName string // substring match against owner username
	Sort      string // "createdDate" (default) | "updatedDate" | "name" | "schematics"
	Order     string // "asc" | "desc" (default)
	Limit     int
	Offset    int
}

const projectMaxName = 200
const projectMaxDescription = 5000

func projectColumns() string {
	return "p.id, p.name, p.description, p.owner_id, u.username, p.created_at, p.updated_at, " +
		`COALESCE((SELECT COUNT(*) FROM project_schematics ps WHERE ps.project_id = p.id), 0)`
}

const projectFrom = "FROM projects p LEFT JOIN users u ON u.id = p.owner_id"

func scanProject(row interface {
	Scan(dest ...any) error
}) (Project, error) {
	var p Project
	var created, updated string
	var ownerID, ownerName sql.NullString
	if err := row.Scan(
		&p.ID, &p.Name, &p.Description, &ownerID, &ownerName,
		&created, &updated, &p.SchematicCount,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return Project{}, err
	}
	p.OwnerID = ownerID.String
	p.OwnerName = ownerName.String
	var err error
	if p.CreatedAt, err = parseTime(created); err != nil {
		return Project{}, err
	}
	if p.UpdatedAt, err = parseTime(updated); err != nil {
		return Project{}, err
	}
	return p, nil
}

// CanManageProject reports whether u may edit or delete p.
func CanManageProject(p Project, u User) bool {
	return u.IsAdmin || p.OwnerID == "" || p.OwnerID == u.ID
}

// CreateProject inserts a new project owned by ownerID.
func (s *Store) CreateProject(ownerID, name, description string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > projectMaxName {
		return Project{}, fmt.Errorf("name must be 1-%d characters", projectMaxName)
	}
	description = strings.TrimSpace(description)
	if len(description) > projectMaxDescription {
		return Project{}, fmt.Errorf("description must be <= %d characters", projectMaxDescription)
	}
	now := time.Now().UTC()
	p := Project{
		ID:          newID(),
		Name:        name,
		Description: description,
		OwnerID:     ownerID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.Exec(
		`INSERT INTO projects (id, name, description, owner_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Description, nullIfEmpty(p.OwnerID), formatTime(now), formatTime(now),
	)
	if err != nil {
		return Project{}, err
	}
	return p, nil
}

// GetProject returns a single project by ID (with its schematic count).
func (s *Store) GetProject(id string) (Project, error) {
	if !validID(id) {
		return Project{}, ErrNotFound
	}
	return scanProject(s.db.QueryRow(`SELECT `+projectColumns()+` `+projectFrom+` WHERE p.id = ?`, id))
}

// UpdateProject changes name and/or description. Nil fields are left untouched.
func (s *Store) UpdateProject(id string, name, description *string) (Project, error) {
	if !validID(id) {
		return Project{}, ErrNotFound
	}
	var sets []string
	var args []any
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || len(n) > projectMaxName {
			return Project{}, fmt.Errorf("name must be 1-%d characters", projectMaxName)
		}
		sets = append(sets, "name = ?")
		args = append(args, n)
	}
	if description != nil {
		d := strings.TrimSpace(*description)
		if len(d) > projectMaxDescription {
			return Project{}, fmt.Errorf("description must be <= %d characters", projectMaxDescription)
		}
		sets = append(sets, "description = ?")
		args = append(args, d)
	}
	if len(sets) == 0 {
		return s.GetProject(id)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, formatTime(time.Now().UTC()), id)
	res, err := s.db.Exec(`UPDATE projects SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return Project{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Project{}, err
	} else if n == 0 {
		return Project{}, ErrNotFound
	}
	return s.GetProject(id)
}

// DeleteProject removes a project and its membership rows (not the schematics).
func (s *Store) DeleteProject(id string) error {
	if !validID(id) {
		return ErrNotFound
	}
	if _, err := s.db.Exec(`DELETE FROM project_schematics WHERE project_id = ?`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListProjects queries the project index with filter/sort/pagination.
func (s *Store) ListProjects(f ProjectFilter) ([]Project, int, error) {
	var conds []string
	var args []any
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + escapeLike(strings.ToLower(q)) + "%"
		conds = append(conds, `(LOWER(p.name) LIKE ? ESCAPE '\' OR LOWER(p.description) LIKE ? ESCAPE '\')`)
		args = append(args, like, like)
	}
	if f.OwnerID != "" {
		conds = append(conds, `p.owner_id = ?`)
		args = append(args, f.OwnerID)
	}
	if o := strings.TrimSpace(f.OwnerName); o != "" {
		like := "%" + escapeLike(strings.ToLower(o)) + "%"
		conds = append(conds, `LOWER(u.username) LIKE ? ESCAPE '\'`)
		args = append(args, like)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) `+projectFrom+` `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	var orderBy string
	switch f.Sort {
	case "name":
		orderBy = "p.name COLLATE NOCASE"
	case "updatedDate":
		orderBy = "p.updated_at"
	case "schematics":
		orderBy = `COALESCE((SELECT COUNT(*) FROM project_schematics ps WHERE ps.project_id = p.id), 0)`
	default:
		orderBy = "p.created_at"
	}
	order := "DESC"
	if strings.EqualFold(f.Order, "asc") {
		order = "ASC"
	}
	query := `SELECT ` + projectColumns() + ` ` + projectFrom + ` ` + where +
		` ORDER BY ` + orderBy + ` ` + order + `, p.id ASC`
	if f.Limit > 0 {
		limit := f.Limit
		if limit > MaxListLimit {
			limit = MaxListLimit
		}
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	if f.Offset > 0 {
		if f.Limit <= 0 {
			query += ` LIMIT -1`
		}
		query += ` OFFSET ?`
		args = append(args, f.Offset)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListProjectSchematics returns a project's schematics in stored order.
func (s *Store) ListProjectSchematics(projectID, viewerID string) ([]SchematicMetadata, error) {
	if !validID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(
		`SELECT `+metaColumns(viewerID)+` FROM project_schematics ps `+
			`JOIN schematics s ON s.id = ps.schematic_id `+
			`LEFT JOIN users u ON u.id = s.owner_id `+
			`WHERE ps.project_id = ? ORDER BY ps.position ASC, ps.added_at ASC, s.id ASC`,
		viewerID, viewerID, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SchematicMetadata{}
	for rows.Next() {
		m, err := scanMetadata(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// ProjectDetail fetches a project and its ordered schematics.
func (s *Store) ProjectDetail(id, viewerID string) (ProjectDetail, error) {
	p, err := s.GetProject(id)
	if err != nil {
		return ProjectDetail{}, err
	}
	items, err := s.ListProjectSchematics(id, viewerID)
	if err != nil {
		return ProjectDetail{}, err
	}
	return ProjectDetail{Project: p, Schematics: items}, nil
}

// ProjectsForSchematic lists the projects a schematic belongs to.
func (s *Store) ProjectsForSchematic(schematicID string) ([]Project, error) {
	if !validID(schematicID) {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT `+projectColumns()+` FROM project_schematics ps `+
			`JOIN projects p ON p.id = ps.project_id `+
			`LEFT JOIN users u ON u.id = p.owner_id `+
			`WHERE ps.schematic_id = ? ORDER BY p.name COLLATE NOCASE`,
		schematicID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// AddProjectSchematic appends a schematic to a project (idempotent).
func (s *Store) AddProjectSchematic(projectID, schematicID string) error {
	if !validID(projectID) || !validID(schematicID) {
		return ErrNotFound
	}
	var maxPos sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT MAX(position) FROM project_schematics WHERE project_id = ?`, projectID,
	).Scan(&maxPos); err != nil {
		return err
	}
	pos := maxPos.Int64 + 1
	_, err := s.db.Exec(
		`INSERT INTO project_schematics (project_id, schematic_id, position, added_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(project_id, schematic_id) DO NOTHING`,
		projectID, schematicID, pos, formatTime(time.Now().UTC()),
	)
	return err
}

// RemoveProjectSchematic removes a schematic from a project.
func (s *Store) RemoveProjectSchematic(projectID, schematicID string) error {
	if !validID(projectID) || !validID(schematicID) {
		return ErrNotFound
	}
	_, err := s.db.Exec(
		`DELETE FROM project_schematics WHERE project_id = ? AND schematic_id = ?`,
		projectID, schematicID,
	)
	return err
}

// SetProjectSchematics replaces a project's membership with schematicIDs in
// the given order. Unknown IDs are ignored.
func (s *Store) SetProjectSchematics(projectID string, schematicIDs []string) error {
	if !validID(projectID) {
		return ErrNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM project_schematics WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	seen := map[string]bool{}
	pos := 0
	for _, sid := range schematicIDs {
		if !validID(sid) || seen[sid] {
			continue
		}
		seen[sid] = true
		if _, err := tx.Exec(
			`INSERT INTO project_schematics (project_id, schematic_id, position, added_at)
			 VALUES (?, ?, ?, ?)`,
			projectID, sid, pos, now,
		); err != nil {
			return err
		}
		pos++
	}
	return tx.Commit()
}

// ProjectHasSchematic reports whether schematicID is already in projectID.
func (s *Store) ProjectHasSchematic(projectID, schematicID string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM project_schematics WHERE project_id = ? AND schematic_id = ?`,
		projectID, schematicID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
