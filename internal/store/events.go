package store

import (
	"context"
	"database/sql"
	"time"
)

// Event kinds.
const (
	EventDeliveryError = "delivery_error"
	EventDiverged      = "diverged"
	EventArchivePush   = "archive_push"
)

// Event is a record of the project log shown in the web interface.
type Event struct {
	ID         int64
	ProjectID  int64
	TargetID   int64
	TargetName string
	Kind       string
	Ref        string
	ArchiveRef string
	Message    string
	CreatedAt  time.Time
}

// AddEvent stores an event.
func (s *Store) AddEvent(ctx context.Context, e *Event) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	e.CreatedAt = time.UnixMilli(e.CreatedAt.UnixMilli())
	res, err := s.q.ExecContext(ctx, `INSERT INTO events
		(project_id, target_id, target_name, kind, ref, archive_ref, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ProjectID, nullID(e.TargetID), e.TargetName, e.Kind, e.Ref, e.ArchiveRef, e.Message, ms(e.CreatedAt))
	if err != nil {
		return err
	}
	e.ID, err = res.LastInsertId()
	return err
}

// ListEvents returns up to limit newest events of a project.
func (s *Store) ListEvents(ctx context.Context, projectID int64, limit int) ([]Event, error) {
	return collect(ctx, s.q, func(r *sql.Rows) (Event, error) {
		var e Event
		var targetID, created sql.NullInt64
		err := r.Scan(&e.ID, &e.ProjectID, &targetID, &e.TargetName, &e.Kind, &e.Ref, &e.ArchiveRef, &e.Message, &created)
		e.TargetID, e.CreatedAt = targetID.Int64, fromMS(created)
		return e, err
	}, `SELECT id, project_id, target_id, target_name, kind, ref, archive_ref, message, created_at
		FROM events WHERE project_id = ? ORDER BY id DESC LIMIT ?`, projectID, limit)
}
