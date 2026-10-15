package store

import (
	"context"
	"database/sql"
	"time"
)

// Project is a git repository of metagit.
type Project struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

func scanProject(r scanner) (Project, error) {
	var p Project
	var created sql.NullInt64
	err := r.Scan(&p.ID, &p.Name, &created)
	p.CreatedAt = fromMS(created)
	return p, err
}

// CreateProject stores a project; ErrConflict if the name is taken.
func (s *Store) CreateProject(ctx context.Context, name string) (Project, error) {
	now := time.Now()
	res, err := s.q.ExecContext(ctx, `INSERT INTO projects (name, created_at) VALUES (?, ?)`, name, ms(now))
	if err != nil {
		if isUnique(err) {
			return Project{}, ErrConflict
		}
		return Project{}, err
	}
	id, err := res.LastInsertId()
	return Project{ID: id, Name: name, CreatedAt: time.UnixMilli(now.UnixMilli())}, err
}

// ListProjects returns all projects ordered by name.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	return collect(ctx, s.q, func(r *sql.Rows) (Project, error) { return scanProject(r) },
		`SELECT id, name, created_at FROM projects ORDER BY name`)
}

// ProjectByName finds a project by name.
func (s *Store) ProjectByName(ctx context.Context, name string) (*Project, error) {
	p, err := scanProject(s.q.QueryRowContext(ctx, `SELECT id, name, created_at FROM projects WHERE name = ?`, name))
	if err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

// ProjectByID finds a project by id.
func (s *Store) ProjectByID(ctx context.Context, id int64) (*Project, error) {
	p, err := scanProject(s.q.QueryRowContext(ctx, `SELECT id, name, created_at FROM projects WHERE id = ?`, id))
	if err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

// DeleteProject removes a project with its targets, archive records and events.
func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	return affected(s.q.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id))
}

// ProtectedRef is a pattern of refs that must not be rewritten or deleted.
type ProtectedRef struct {
	ID        int64
	ProjectID int64
	Pattern   string
}

// ListProtected returns protected ref patterns of a project.
func (s *Store) ListProtected(ctx context.Context, projectID int64) ([]ProtectedRef, error) {
	return collect(ctx, s.q, func(r *sql.Rows) (ProtectedRef, error) {
		var p ProtectedRef
		err := r.Scan(&p.ID, &p.ProjectID, &p.Pattern)
		return p, err
	}, `SELECT id, project_id, pattern FROM protected_refs WHERE project_id = ? ORDER BY id`, projectID)
}

// AddProtected adds a pattern; ErrConflict if it already exists.
func (s *Store) AddProtected(ctx context.Context, projectID int64, pattern string) (ProtectedRef, error) {
	res, err := s.q.ExecContext(ctx, `INSERT INTO protected_refs (project_id, pattern) VALUES (?, ?)`, projectID, pattern)
	if err != nil {
		if isUnique(err) {
			return ProtectedRef{}, ErrConflict
		}
		return ProtectedRef{}, err
	}
	id, err := res.LastInsertId()
	return ProtectedRef{ID: id, ProjectID: projectID, Pattern: pattern}, err
}

// DeleteProtected removes a pattern of the project.
func (s *Store) DeleteProtected(ctx context.Context, projectID, id int64) error {
	return affected(s.q.ExecContext(ctx, `DELETE FROM protected_refs WHERE id = ? AND project_id = ?`, id, projectID))
}
