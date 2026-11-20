package repository

import (
	"context"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (store *Store) CreateRule(ctx context.Context, projectID string, rule entity.Rule, maxRules int) (*entity.Rule, error) {
	rule.ID = secure.Random(12)
	rule.ProjectID = projectID
	rule.Version = 1
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		var locked string
		if err := transaction.NewRaw(`SELECT id FROM projects WHERE id = ? FOR UPDATE`, projectID).Scan(ctx, &locked); err != nil {
			return err
		}
		var count int
		if err := transaction.NewRaw(`SELECT count(*) FROM redaction_rules WHERE project_id = ?`, projectID).Scan(ctx, &count); err != nil {
			return err
		}
		if count >= maxRules {
			return ErrRuleLimit
		}
		if _, err := transaction.NewInsert().Model(&rule).Exec(ctx); err != nil {
			return err
		}
		return bumpRulesVersion(ctx, transaction, projectID)
	})
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func bumpRulesVersion(ctx context.Context, transaction bun.Tx, projectID string) error {
	_, err := transaction.NewUpdate().Model((*entity.Project)(nil)).
		Set("rules_version = rules_version + 1").Where("id = ?", projectID).Exec(ctx)
	return err
}

func (store *Store) Rule(ctx context.Context, projectID, id string) (*entity.Rule, error) {
	rule := new(entity.Rule)
	if err := store.DB.NewSelect().Model(rule).Where("id = ? AND project_id = ?", id, projectID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return rule, nil
}

// UpdateRule replaces a rule when its version still matches; the stale writer gets ErrVersionConflict.
func (store *Store) UpdateRule(ctx context.Context, projectID, id string, rule entity.Rule, version int64) (*entity.Rule, error) {
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		result, err := transaction.NewUpdate().Model((*entity.Rule)(nil)).
			Set("kind = ?, detector = ?, pattern = ?, capture = ?, label = ?, action = ?, enabled = ?, version = version + 1", rule.Kind, rule.Detector, rule.Pattern, rule.Capture, rule.Label, rule.Action, rule.Enabled).
			Where("id = ? AND project_id = ? AND version = ?", id, projectID, version).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrVersionConflict
		}
		return bumpRulesVersion(ctx, transaction, projectID)
	})
	if err != nil {
		return nil, err
	}
	return store.Rule(ctx, projectID, id)
}

func (store *Store) DeleteRule(ctx context.Context, projectID, id, action string, version int64) error {
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		result, err := transaction.NewDelete().Model((*entity.Rule)(nil)).
			Where("id = ? AND project_id = ? AND action = ? AND version = ?", id, projectID, action, version).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrVersionConflict
		}
		return bumpRulesVersion(ctx, transaction, projectID)
	})
}

func (store *Store) Rules(ctx context.Context, projectID string) ([]entity.Rule, error) {
	rules := []entity.Rule{}
	if err := store.DB.NewSelect().Model(&rules).Where("project_id = ?", projectID).Order("label", "id").Scan(ctx); err != nil {
		return nil, err
	}
	return rules, nil
}
