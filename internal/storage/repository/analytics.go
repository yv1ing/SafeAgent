package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (store *Store) ActivePolicy(ctx context.Context, projectID string) (*entity.PolicyVersion, error) {
	policy := new(entity.PolicyVersion)
	err := store.DB.NewSelect().Model(policy).Where("project_id = ?", projectID).Order("version DESC").Limit(1).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return policy, nil
}

func (store *Store) PolicyVersions(ctx context.Context, projectID string) ([]entity.PolicyVersion, error) {
	versions := []entity.PolicyVersion{}
	err := store.DB.NewSelect().Model(&versions).Where("project_id = ?", projectID).Order("version DESC").Scan(ctx)
	return versions, err
}

func (store *Store) PolicyVersion(ctx context.Context, projectID string, version int64) (*entity.PolicyVersion, error) {
	policy := new(entity.PolicyVersion)
	if err := store.DB.NewSelect().Model(policy).Where("project_id = ? AND version = ?", projectID, version).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return policy, nil
}

func (store *Store) SavePolicyVersion(ctx context.Context, policy *entity.PolicyVersion) (int64, error) {
	var version int64
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if err := transaction.NewRaw(`SELECT COALESCE(max(version), 0) + 1 FROM policy_versions WHERE project_id = ?`, policy.ProjectID).Scan(ctx, &version); err != nil {
			return err
		}
		policy.Version = version
		if _, err := transaction.NewInsert().Model(policy).Exec(ctx); err != nil {
			return err
		}
		_, err := transaction.NewUpdate().Model((*entity.Project)(nil)).
			Set("active_policy_version = ?", version).Where("id = ?", policy.ProjectID).Exec(ctx)
		return err
	})
	return version, err
}

func (store *Store) ActivePolicies(ctx context.Context) ([]entity.PolicyVersion, error) {
	policies := []entity.PolicyVersion{}
	err := store.DB.NewRaw(`SELECT DISTINCT ON (project_id) * FROM policy_versions ORDER BY project_id, version DESC`).Scan(ctx, &policies)
	return policies, err
}

type TopAgent struct {
	ProjectID    string    `bun:"project_id"`
	AgentKeyID   string    `bun:"agent_key_id"`
	Label        string    `bun:"label"`
	Calls        int64     `bun:"calls"`
	Blocked      int64     `bun:"blocked"`
	HighFindings int64     `bun:"high_findings"`
	LastAt       time.Time `bun:"last_at"`
}

func (store *Store) TopAgents(ctx context.Context, projectIDs []string, since time.Time, limit int) ([]TopAgent, error) {
	rows := []TopAgent{}
	if len(projectIDs) == 0 {
		return rows, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	err := store.DB.NewRaw(`SELECT c.project_id, c.agent_key_id, COALESCE(k.label, c.agent_key_id) AS label,
			count(*) AS calls, count(*) FILTER (WHERE c.outcome = 'blocked') AS blocked,
			count(*) FILTER (WHERE c.max_severity = 'high') AS high_findings, max(c.created_at) AS last_at
		FROM calls c LEFT JOIN agent_keys k ON k.id = c.agent_key_id
		WHERE c.project_id IN (?) AND c.created_at >= ? AND c.lane = 'main'
		GROUP BY c.project_id, c.agent_key_id, k.label
		ORDER BY high_findings DESC, calls DESC LIMIT ?`, bunIn(projectIDs), since, limit).Scan(ctx, &rows)
	return rows, err
}

type CallStats struct {
	ProjectID    string    `bun:"project_id"`
	Calls        int64     `bun:"calls"`
	Failures     int64     `bun:"failures"`
	Blocked      int64     `bun:"blocked"`
	Findings     int64     `bun:"findings"`
	HighFindings int64     `bun:"high_findings"`
	LastAt       time.Time `bun:"last_at"`
}

func (store *Store) CallStats(ctx context.Context, projectIDs []string, since time.Time) ([]CallStats, error) {
	rows := []CallStats{}
	if len(projectIDs) == 0 {
		return rows, nil
	}
	err := store.DB.NewRaw(`SELECT project_id, count(*) AS calls,
			count(*) FILTER (WHERE state = 'failed') AS failures,
			count(*) FILTER (WHERE outcome = 'blocked') AS blocked,
			COALESCE(sum(finding_count), 0)::bigint AS findings,
			count(*) FILTER (WHERE max_severity = 'high') AS high_findings,
			max(created_at) AS last_at
		FROM calls WHERE project_id IN (?) AND created_at >= ? AND lane = 'main'
		GROUP BY project_id`, bunIn(projectIDs), since).Scan(ctx, &rows)
	return rows, err
}
