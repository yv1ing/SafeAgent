package repository

import (
	"context"
	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"time"
)

const (
	pruneBatch         = 1000
	quotaRetentionDays = 7
)

func (store *Store) VisibleProjects(ctx context.Context, userID string, administrator bool) ([]entity.Project, error) {
	projects := []entity.Project{}
	query := store.DB.NewSelect().Model(&projects).Order("name")
	if !administrator {
		query = query.Where("id IN (SELECT project_id FROM project_members WHERE user_id = ?)", userID)
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	return projects, nil
}

func (store *Store) CreateProject(ctx context.Context, project *entity.Project, rules []entity.Rule, policy *entity.PolicyVersion) (*entity.Project, error) {
	project.ID = secure.Random(12)
	project.Version = 1
	project.CreatedAt = time.Now().UTC()
	if policy != nil {
		project.ActivePolicyVersion = 1
	}
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if _, err := transaction.NewInsert().Model(project).Exec(ctx); err != nil {
			return err
		}
		for index := range rules {
			rules[index].ProjectID = project.ID
		}
		if len(rules) > 0 {
			if _, err := transaction.NewInsert().Model(&rules).Exec(ctx); err != nil {
				return err
			}
		}
		if policy == nil {
			return nil
		}
		policy.ProjectID, policy.Version = project.ID, 1
		_, err := transaction.NewInsert().Model(policy).Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return project, nil
}

func (store *Store) UpdateProject(ctx context.Context, id string, project entity.Project, version int64) (*entity.Project, error) {
	result, err := store.DB.NewUpdate().Model((*entity.Project)(nil)).
		Set("name = ?, retention_days = ?, auxiliary_retention_days = ?, default_route_id = ?, requests_per_minute = ?, concurrency = ?, daily_token_quota = ?, default_max_output_tokens = ?, stream_review_bytes = ?, stream_review_timeout_ms = ?, coverage_threshold = ?, version = version + 1",
			project.Name, project.RetentionDays, project.AuxiliaryRetentionDays, project.DefaultRouteID,
			project.RequestsPerMinute, project.Concurrency, project.DailyTokenQuota,
			project.DefaultMaxOutputTokens, project.StreamReviewBytes, project.StreamReviewTimeoutMS, project.CoverageThreshold).
		Where("id = ? AND version = ?", id, version).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrVersionConflict
	}
	return store.Project(ctx, id)
}

func (store *Store) DeleteProject(ctx context.Context, id string) error {
	var found bool
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw(`DELETE FROM findings WHERE trajectory_id IN (SELECT id FROM trajectory_summaries WHERE project_id = ?)`, id).Exec(ctx); err != nil {
			return err
		}
		for _, statement := range []string{
			`DELETE FROM edges WHERE trajectory_id IN (SELECT id FROM trajectory_summaries WHERE project_id = ?)`,
			`DELETE FROM segments WHERE trajectory_id IN (SELECT id FROM trajectory_summaries WHERE project_id = ?)`,
			`DELETE FROM trajectory_events WHERE trajectory_id IN (SELECT id FROM trajectory_summaries WHERE project_id = ?)`,
			`DELETE FROM checkpoints WHERE trajectory_id IN (SELECT id FROM trajectory_summaries WHERE project_id = ?)`,
			`DELETE FROM trajectory_states WHERE trajectory_id IN (SELECT id FROM trajectory_summaries WHERE project_id = ?)`,
			`DELETE FROM trajectory_summaries WHERE project_id = ?`,
		} {
			if _, err := tx.NewRaw(statement, id).Exec(ctx); err != nil {
				return err
			}
		}
		result, err := tx.NewDelete().Model((*entity.Project)(nil)).Where("id = ?", id).Exec(ctx)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		found = affected > 0
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func (store *Store) Members(ctx context.Context, projectID string) ([]entity.User, error) {
	users := []entity.User{}
	err := store.DB.NewSelect().Model(&users).
		Where("id IN (SELECT user_id FROM project_members WHERE project_id = ?)", projectID).
		Order("username").Scan(ctx)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (store *Store) AddMember(ctx context.Context, projectID, userID string) error {
	member := &entity.Member{ProjectID: projectID, UserID: userID}
	_, err := store.DB.NewInsert().Model(member).On("CONFLICT DO NOTHING").Exec(ctx)
	return err
}

func (store *Store) RemoveMember(ctx context.Context, projectID, userID string) error {
	result, err := store.DB.NewDelete().Model((*entity.Member)(nil)).Where("project_id = ? AND user_id = ?", projectID, userID).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Prune removes expired rows in bounded batches and returns when nothing is left to remove.
func (store *Store) Prune(ctx context.Context, operationLogDays int) error {
	now := time.Now().UTC()
	statements := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM operation_logs WHERE id IN (SELECT id FROM operation_logs WHERE created_at < ? LIMIT ?)`, []any{now.AddDate(0, 0, -operationLogDays), pruneBatch}},
		{`DELETE FROM sessions WHERE id IN (SELECT id FROM sessions WHERE expires_at < ? LIMIT ?)`, []any{now, pruneBatch}},
		{`DELETE FROM quota_usage WHERE ctid IN (SELECT ctid FROM quota_usage WHERE day < ? LIMIT ?)`, []any{now.AddDate(0, 0, -quotaRetentionDays).Format("2006-01-02"), pruneBatch}},
		{`DELETE FROM decision_model_usage WHERE ctid IN (SELECT ctid FROM decision_model_usage WHERE day < ? LIMIT ?)`, []any{now.AddDate(0, 0, -quotaRetentionDays).Format("2006-01-02"), pruneBatch}},
		{`DELETE FROM traffic_rollups WHERE ctid IN (
			SELECT t.ctid FROM traffic_rollups t JOIN projects p ON p.id = t.project_id
			WHERE t.bucket_start < now() - make_interval(days => p.retention_days::int) - interval '1 day' LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM calls WHERE id IN (
			SELECT c.id FROM calls c JOIN projects p ON p.id = c.project_id
			WHERE c.lane <> 'auxiliary' AND c.created_at < now() - make_interval(days => p.retention_days::int) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM calls WHERE id IN (
			SELECT c.id FROM calls c JOIN projects p ON p.id = c.project_id
			WHERE c.lane = 'auxiliary'
				AND c.created_at < now() - make_interval(days => CASE
					WHEN p.auxiliary_retention_days > 0 THEN p.auxiliary_retention_days
					ELSE p.retention_days::int END) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM findings WHERE trajectory_id IN (
			SELECT s.id FROM trajectory_summaries s JOIN projects p ON p.id = s.project_id
			WHERE s.last_event_at < now() - make_interval(days => p.retention_days::int) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM edges WHERE trajectory_id IN (
			SELECT s.id FROM trajectory_summaries s JOIN projects p ON p.id = s.project_id
			WHERE s.last_event_at < now() - make_interval(days => p.retention_days::int) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM segments WHERE trajectory_id IN (
			SELECT s.id FROM trajectory_summaries s JOIN projects p ON p.id = s.project_id
			WHERE s.last_event_at < now() - make_interval(days => p.retention_days::int) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM trajectory_events WHERE trajectory_id IN (
			SELECT s.id FROM trajectory_summaries s JOIN projects p ON p.id = s.project_id
			WHERE s.last_event_at < now() - make_interval(days => p.retention_days::int) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM checkpoints WHERE trajectory_id IN (
			SELECT s.id FROM trajectory_summaries s JOIN projects p ON p.id = s.project_id
			WHERE s.last_event_at < now() - make_interval(days => p.retention_days::int) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM trajectory_states WHERE trajectory_id IN (
			SELECT s.id FROM trajectory_summaries s JOIN projects p ON p.id = s.project_id
			WHERE s.last_event_at < now() - make_interval(days => p.retention_days::int) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM trajectory_summaries WHERE id IN (
			SELECT s.id FROM trajectory_summaries s JOIN projects p ON p.id = s.project_id
			WHERE s.last_event_at < now() - make_interval(days => p.retention_days::int) LIMIT ?)`, []any{pruneBatch}},
		{`DELETE FROM blobs WHERE ctid IN (SELECT b.ctid FROM blobs b WHERE NOT EXISTS (SELECT 1 FROM segments s WHERE s.bytes_ref = b.hash) LIMIT ?)`, []any{pruneBatch}},
	}
	for _, statement := range statements {
		for {
			result, err := store.DB.NewRaw(statement.query, statement.args...).Exec(ctx)
			if err != nil {
				return err
			}
			if affected, _ := result.RowsAffected(); affected < pruneBatch {
				break
			}
		}
	}
	return nil
}

func (store *Store) Project(ctx context.Context, id string) (*entity.Project, error) {
	project := new(entity.Project)
	if err := store.DB.NewSelect().Model(project).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return project, nil
}

func (store *Store) Membership(ctx context.Context, projectID, userID string) (bool, error) {
	count, err := store.DB.NewSelect().Model((*entity.Member)(nil)).Where("project_id = ? AND user_id = ?", projectID, userID).Count(ctx)
	return count > 0, err
}
