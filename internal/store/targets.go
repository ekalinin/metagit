package store

import (
	"context"
	"database/sql"
	"time"
)

// Authentication kinds of a target.
const (
	AuthNone  = "none"
	AuthHTTPS = "https"
	AuthSSH   = "ssh"
)

// Delivery states of a target.
const (
	StateSynced  = "synced"
	StatePending = "pending"
	StateError   = "error"
)

// Target is a repository on a hosting that receives branches and tags.
type Target struct {
	ID        int64
	ProjectID int64
	Name      string
	URL       string
	AuthKind  string
	Username  string
	// Secret is the encrypted token or SSH private key.
	Secret  []byte
	Enabled bool
	TargetStatus
	CreatedAt time.Time
}

// TargetStatus is the delivery state of a target.
type TargetStatus struct {
	State         string
	LastError     string
	LastSuccessAt time.Time
	NextAttemptAt time.Time
	Attempt       int
}

const targetColumns = `id, project_id, name, url, auth_kind, username, secret, enabled,
	state, last_error, last_success_at, next_attempt_at, attempt, created_at`

func scanTarget(r scanner) (Target, error) {
	var t Target
	var success, next, created sql.NullInt64
	err := r.Scan(&t.ID, &t.ProjectID, &t.Name, &t.URL, &t.AuthKind, &t.Username, &t.Secret, &t.Enabled,
		&t.State, &t.LastError, &success, &next, &t.Attempt, &created)
	t.LastSuccessAt, t.NextAttemptAt, t.CreatedAt = fromMS(success), fromMS(next), fromMS(created)
	return t, err
}

// CreateTarget stores a target and sets its ID; ErrConflict if the project
// already has a target with this name.
func (s *Store) CreateTarget(ctx context.Context, t *Target) error {
	if t.State == "" {
		t.State = StatePending
	}
	t.CreatedAt = time.UnixMilli(time.Now().UnixMilli())
	res, err := s.q.ExecContext(ctx, `INSERT INTO targets
		(project_id, name, url, auth_kind, username, secret, enabled, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ProjectID, t.Name, t.URL, t.AuthKind, t.Username, t.Secret, t.Enabled, t.State, ms(t.CreatedAt))
	if err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	t.ID, err = res.LastInsertId()
	return err
}

// UpdateTarget saves the settings of a target (not its delivery status).
// Changing the URL forgets what was delivered to the old repository.
func (s *Store) UpdateTarget(ctx context.Context, t *Target) error {
	return s.WithTxIfNeeded(ctx, func(tx *Store) error {
		old, err := tx.TargetByID(ctx, t.ID)
		if err != nil {
			return err
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE targets SET name = ?, url = ?, auth_kind = ?, username = ?, secret = ?, enabled = ?
			WHERE id = ?`, t.Name, t.URL, t.AuthKind, t.Username, t.Secret, t.Enabled, t.ID)
		if err != nil {
			if isUnique(err) {
				return ErrConflict
			}
			return err
		}
		if old.URL != t.URL {
			_, err = tx.q.ExecContext(ctx, `DELETE FROM target_refs WHERE target_id = ?`, t.ID)
		}
		return err
	})
}

// WithTxIfNeeded runs fn in a new transaction unless s is already bound to one.
func (s *Store) WithTxIfNeeded(ctx context.Context, fn func(tx *Store) error) error {
	if _, ok := s.q.(*sql.Tx); ok {
		return fn(s)
	}
	return s.WithTx(ctx, fn)
}

// SetTargetStatus saves the delivery status of a target.
func (s *Store) SetTargetStatus(ctx context.Context, id int64, st TargetStatus) error {
	_, err := s.q.ExecContext(ctx, `UPDATE targets SET state = ?, last_error = ?, last_success_at = ?,
		next_attempt_at = ?, attempt = ? WHERE id = ?`,
		st.State, st.LastError, ms(st.LastSuccessAt), ms(st.NextAttemptAt), st.Attempt, id)
	return err
}

// DeleteTarget removes a target of the project.
func (s *Store) DeleteTarget(ctx context.Context, projectID, id int64) error {
	return affected(s.q.ExecContext(ctx, `DELETE FROM targets WHERE id = ? AND project_id = ?`, id, projectID))
}

// TargetByID finds a target.
func (s *Store) TargetByID(ctx context.Context, id int64) (*Target, error) {
	t, err := scanTarget(s.q.QueryRowContext(ctx, `SELECT `+targetColumns+` FROM targets WHERE id = ?`, id))
	if err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

// ListTargets returns targets of a project ordered by name.
func (s *Store) ListTargets(ctx context.Context, projectID int64) ([]Target, error) {
	return collect(ctx, s.q, func(r *sql.Rows) (Target, error) { return scanTarget(r) },
		`SELECT `+targetColumns+` FROM targets WHERE project_id = ? ORDER BY name`, projectID)
}

// ListAllTargets returns targets of all projects.
func (s *Store) ListAllTargets(ctx context.Context) ([]Target, error) {
	return collect(ctx, s.q, func(r *sql.Rows) (Target, error) { return scanTarget(r) },
		`SELECT `+targetColumns+` FROM targets ORDER BY id`)
}

// TargetRefs returns what metagit last delivered to the target: ref -> object.
func (s *Store) TargetRefs(ctx context.Context, targetID int64) (map[string]string, error) {
	type pair struct{ ref, obj string }
	pairs, err := collect(ctx, s.q, func(r *sql.Rows) (pair, error) {
		var p pair
		err := r.Scan(&p.ref, &p.obj)
		return p, err
	}, `SELECT ref, object FROM target_refs WHERE target_id = ?`, targetID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		m[p.ref] = p.obj
	}
	return m, nil
}

// SetTargetRef records that the target has ref pointing to object.
func (s *Store) SetTargetRef(ctx context.Context, targetID int64, ref, object string) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO target_refs (target_id, ref, object) VALUES (?, ?, ?)
		ON CONFLICT (target_id, ref) DO UPDATE SET object = excluded.object`, targetID, ref, object)
	return err
}

// DeleteTargetRef records that the target no longer has the ref.
func (s *Store) DeleteTargetRef(ctx context.Context, targetID int64, ref string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM target_refs WHERE target_id = ? AND ref = ?`, targetID, ref)
	return err
}

// MarkTargetPending switches a synced target to pending. Failed targets keep
// their error until the next delivery finishes.
func (s *Store) MarkTargetPending(ctx context.Context, id int64) error {
	_, err := s.q.ExecContext(ctx, `UPDATE targets SET state = ? WHERE id = ? AND state = ?`, StatePending, id, StateSynced)
	return err
}
