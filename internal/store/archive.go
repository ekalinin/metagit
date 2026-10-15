package store

import (
	"context"
	"database/sql"
	"time"
)

// ArchiveIntent is a destructive ref update accepted in pre-receive whose
// previous value must be archived after the push.
type ArchiveIntent struct {
	ID        int64
	ProjectID int64
	PushID    string
	Ref       string
	OldObject string
	NewObject string
	CreatedAt time.Time
}

// AddArchiveIntent stores an intent.
func (s *Store) AddArchiveIntent(ctx context.Context, in *ArchiveIntent) error {
	in.CreatedAt = time.UnixMilli(time.Now().UnixMilli())
	res, err := s.q.ExecContext(ctx, `INSERT INTO archive_intents
		(project_id, push_id, ref, old_object, new_object, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		in.ProjectID, in.PushID, in.Ref, in.OldObject, in.NewObject, ms(in.CreatedAt))
	if err != nil {
		return err
	}
	in.ID, err = res.LastInsertId()
	return err
}

func scanIntent(r *sql.Rows) (ArchiveIntent, error) {
	var in ArchiveIntent
	var created sql.NullInt64
	err := r.Scan(&in.ID, &in.ProjectID, &in.PushID, &in.Ref, &in.OldObject, &in.NewObject, &created)
	in.CreatedAt = fromMS(created)
	return in, err
}

const intentColumns = `id, project_id, push_id, ref, old_object, new_object, created_at`

// PendingIntents returns unfinished intents of a push.
func (s *Store) PendingIntents(ctx context.Context, pushID string) ([]ArchiveIntent, error) {
	return collect(ctx, s.q, scanIntent,
		`SELECT `+intentColumns+` FROM archive_intents WHERE done = 0 AND push_id = ? ORDER BY id`, pushID)
}

// AllPendingIntents returns every unfinished intent.
func (s *Store) AllPendingIntents(ctx context.Context) ([]ArchiveIntent, error) {
	return collect(ctx, s.q, scanIntent,
		`SELECT `+intentColumns+` FROM archive_intents WHERE done = 0 ORDER BY id`)
}

// FinishIntent marks an intent as processed.
func (s *Store) FinishIntent(ctx context.Context, id int64) error {
	_, err := s.q.ExecContext(ctx, `UPDATE archive_intents SET done = 1 WHERE id = ?`, id)
	return err
}

// Archive sources.
const (
	SourcePush   = "push"
	SourceTarget = "target"
)

// ArchiveRef describes a ref under refs/metagit/archive/.
type ArchiveRef struct {
	ID         int64
	ProjectID  int64
	Source     string
	TargetID   int64
	TargetName string
	OrigRef    string
	ArchiveRef string
	Object     string
	CreatedAt  time.Time
}

// AddArchiveRef stores an archive record.
func (s *Store) AddArchiveRef(ctx context.Context, a *ArchiveRef) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	a.CreatedAt = time.UnixMilli(a.CreatedAt.UnixMilli())
	res, err := s.q.ExecContext(ctx, `INSERT INTO archive_refs
		(project_id, source, target_id, target_name, orig_ref, archive_ref, object, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ProjectID, a.Source, nullID(a.TargetID), a.TargetName, a.OrigRef, a.ArchiveRef, a.Object, ms(a.CreatedAt))
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	return err
}

// ListArchiveRefs returns archive records of a project, newest first.
func (s *Store) ListArchiveRefs(ctx context.Context, projectID int64) ([]ArchiveRef, error) {
	return collect(ctx, s.q, func(r *sql.Rows) (ArchiveRef, error) {
		var a ArchiveRef
		var targetID sql.NullInt64
		var created sql.NullInt64
		err := r.Scan(&a.ID, &a.ProjectID, &a.Source, &targetID, &a.TargetName, &a.OrigRef, &a.ArchiveRef, &a.Object, &created)
		a.TargetID, a.CreatedAt = targetID.Int64, fromMS(created)
		return a, err
	}, `SELECT id, project_id, source, target_id, target_name, orig_ref, archive_ref, object, created_at
		FROM archive_refs WHERE project_id = ? ORDER BY id DESC`, projectID)
}
