package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// SaveAnalysis stores one analysis, its findings and flows, and updates the call projection in one
// transaction. Retrying the same identity is idempotent.
func (store *Store) SaveAnalysis(ctx context.Context, analysis *entity.Analysis, findings []entity.AnalysisFinding, flows []entity.AnalysisFlow) error {
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		result, err := transaction.NewInsert().Model(analysis).
			On("CONFLICT (call_id, input_hash, detector_version, decision_model) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return nil
		}
		if len(findings) > 0 {
			if _, err := transaction.NewInsert().Model(&findings).
				On("CONFLICT (analysis_id, fingerprint) DO NOTHING").Exec(ctx); err != nil {
				return err
			}
		}
		if len(flows) > 0 {
			if _, err := transaction.NewInsert().Model(&flows).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
				return err
			}
		}
		maxSeverity := ""
		for _, finding := range findings {
			if severityRank(finding.Severity) > severityRank(maxSeverity) {
				maxSeverity = finding.Severity
			}
		}
		_, err = transaction.NewUpdate().Model((*entity.Call)(nil)).
			Set("latest_analysis_id = ?", analysis.ID).
			Set("analysis_state = ?", analysis.State).
			Set("finding_count = ?", len(findings)).
			Set("max_severity = ?", maxSeverity).
			Where(`id = ? AND (latest_analysis_id = '' OR NOT EXISTS (
				SELECT 1 FROM analyses current WHERE current.id = latest_analysis_id AND current.started_at > ?))`,
				analysis.CallID, analysis.StartedAt).
			Exec(ctx)
		return err
	})
}

func severityRank(severity string) int {
	switch severity {
	case "high":
		return 3
	case "medium":
		return 2
	case "info":
		return 1
	}
	return 0
}

func (store *Store) AnalysisByIdentity(ctx context.Context, callID, inputHash, detectorVersion, decisionModel string) (*entity.Analysis, error) {
	analysis := new(entity.Analysis)
	err := store.DB.NewSelect().Model(analysis).
		Where("call_id = ? AND input_hash = ? AND detector_version = ? AND decision_model = ?", callID, inputHash, detectorVersion, decisionModel).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return analysis, nil
}

func (store *Store) Analysis(ctx context.Context, projectID, analysisID string) (*entity.Analysis, error) {
	analysis := new(entity.Analysis)
	if err := store.DB.NewSelect().Model(analysis).Where("id = ? AND project_id = ?", analysisID, projectID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return analysis, nil
}

func (store *Store) AnalysesOfCall(ctx context.Context, projectID, callID string, limit int) ([]entity.Analysis, error) {
	analyses := []entity.Analysis{}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	err := store.DB.NewSelect().Model(&analyses).
		Where("project_id = ? AND call_id = ?", projectID, callID).
		Order("started_at DESC").Limit(limit).Scan(ctx)
	return analyses, err
}

func (store *Store) FindingsOfAnalysis(ctx context.Context, analysisID string) ([]entity.AnalysisFinding, error) {
	findings := []entity.AnalysisFinding{}
	err := store.DB.NewSelect().Model(&findings).Where("analysis_id = ?", analysisID).
		OrderExpr("CASE severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END DESC").
		Order("created_at", "code").Scan(ctx)
	return findings, err
}

func (store *Store) FlowsOfAnalysis(ctx context.Context, analysisID string) ([]entity.AnalysisFlow, error) {
	flows := []entity.AnalysisFlow{}
	err := store.DB.NewSelect().Model(&flows).Where("analysis_id = ?", analysisID).
		Order("created_at", "id").Scan(ctx)
	return flows, err
}

func (store *Store) DecisionsOfCall(ctx context.Context, callID string) ([]entity.Decision, error) {
	decisions := []entity.Decision{}
	err := store.DB.NewSelect().Model(&decisions).Where("call_id = ?", callID).Order("created_at").Scan(ctx)
	return decisions, err
}

// ActivePolicy returns the newest saved policy of a project, or nil when the project runs the
// built-in empty document.
func (store *Store) ActivePolicy(ctx context.Context, projectID string) (*entity.PolicyVersion, error) {
	policy := new(entity.PolicyVersion)
	err := store.DB.NewSelect().Model(policy).
		Where("project_id = ?", projectID).Order("version DESC").Limit(1).Scan(ctx)
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
	err := store.DB.NewSelect().Model(&versions).Where("project_id = ?", projectID).
		Order("version DESC").Scan(ctx)
	return versions, err
}

func (store *Store) PolicyVersion(ctx context.Context, projectID string, version int64) (*entity.PolicyVersion, error) {
	policy := new(entity.PolicyVersion)
	if err := store.DB.NewSelect().Model(policy).Where("project_id = ? AND version = ?", projectID, version).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return policy, nil
}

// SavePolicyVersion allocates the next version, inserts it and moves the project pointer in one
// transaction. The project row is locked first so two concurrent saves cannot choose the same
// version. Saving is activation: there is no draft state.
func (store *Store) SavePolicyVersion(ctx context.Context, policy *entity.PolicyVersion) (int64, error) {
	return store.savePolicyVersion(ctx, policy, -1)
}

// SavePolicyVersionChecked also refuses to activate when the project's active version changed since
// the caller validated a member document.
func (store *Store) SavePolicyVersionChecked(ctx context.Context, policy *entity.PolicyVersion, expected int64) (int64, error) {
	return store.savePolicyVersion(ctx, policy, expected)
}

func (store *Store) savePolicyVersion(ctx context.Context, policy *entity.PolicyVersion, expected int64) (int64, error) {
	var version int64
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		var active int64
		if err := transaction.NewRaw(`SELECT active_policy_version FROM projects WHERE id = ? FOR UPDATE`, policy.ProjectID).Scan(ctx, &active); err != nil {
			return err
		}
		if expected >= 0 && active != expected {
			return ErrVersionConflict
		}
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

// ActivePolicies returns the newest version of every project for the startup compilation check.
func (store *Store) ActivePolicies(ctx context.Context) ([]entity.PolicyVersion, error) {
	policies := []entity.PolicyVersion{}
	err := store.DB.NewRaw(`SELECT DISTINCT ON (project_id) * FROM policy_versions
		ORDER BY project_id, version DESC`).Scan(ctx, &policies)
	return policies, err
}

// ProjectAnalysisStats aggregates counts for the monitor page.
// TopAgent is one row of the monitor's agent leaderboard.
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
			count(*) AS calls, count(*) FILTER (WHERE c.state = 'blocked') AS blocked,
			count(*) FILTER (WHERE c.max_severity = 'high') AS high_findings, max(c.created_at) AS last_at
		FROM calls c LEFT JOIN agent_keys k ON k.id = c.agent_key_id
		WHERE c.project_id IN (?) AND c.created_at >= ?
		GROUP BY c.project_id, c.agent_key_id, k.label
		ORDER BY high_findings DESC, calls DESC LIMIT ?`, bun.In(projectIDs), since, limit).Scan(ctx, &rows)
	return rows, err
}

// HighRiskCall is one row of the monitor's recent-risk feed.
type HighRiskCall struct {
	CallID     string    `bun:"call_id"`
	ProjectID  string    `bun:"project_id"`
	AgentKeyID string    `bun:"agent_key_id"`
	Model      string    `bun:"model"`
	Severity   string    `bun:"severity"`
	State      string    `bun:"state"`
	CreatedAt  time.Time `bun:"created_at"`
}

func (store *Store) HighRiskCalls(ctx context.Context, projectIDs []string, since time.Time, limit int) ([]HighRiskCall, error) {
	rows := []HighRiskCall{}
	if len(projectIDs) == 0 {
		return rows, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	err := store.DB.NewRaw(`SELECT id AS call_id, project_id, agent_key_id, public_model AS model,
			max_severity AS severity, state, created_at
		FROM calls WHERE project_id IN (?) AND created_at >= ? AND max_severity = 'high'
		ORDER BY created_at DESC LIMIT ?`, bun.In(projectIDs), since, limit).Scan(ctx, &rows)
	return rows, err
}

// CallStats is the windowed per-project traffic aggregate used by the monitor.
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
			count(*) FILTER (WHERE state = 'blocked') AS blocked,
			COALESCE(sum(finding_count), 0)::bigint AS findings,
			count(*) FILTER (WHERE max_severity = 'high') AS high_findings,
			max(created_at) AS last_at
		FROM calls WHERE project_id IN (?) AND created_at >= ?
		GROUP BY project_id`, bun.In(projectIDs), since).Scan(ctx, &rows)
	return rows, err
}

// SnapshotOfCall returns the immutable redaction rules and hits captured when a call was admitted.
func (store *Store) SnapshotOfCall(ctx context.Context, callID string) (*entity.RedactionSnapshot, error) {
	snapshot := new(entity.RedactionSnapshot)
	if err := store.DB.NewSelect().Model(snapshot).Where("call_id = ?", callID).Order("created_at DESC").Limit(1).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return snapshot, nil
}

// AgentRisk aggregates findings by agent key for the risk page.
type AgentRisk struct {
	AgentKeyID string    `bun:"agent_key_id" json:"agent_key_id"`
	Label      string    `bun:"label" json:"label"`
	Findings   int64     `bun:"findings" json:"findings"`
	High       int64     `bun:"high" json:"high"`
	Flows      int64     `bun:"flows" json:"flows"`
	LastAt     time.Time `bun:"last_at" json:"last_at"`
}

func (store *Store) AgentRisk(ctx context.Context, projectID string, since time.Time) ([]AgentRisk, error) {
	rows := []AgentRisk{}
	err := store.DB.NewRaw(`SELECT c.agent_key_id, COALESCE(k.label, c.agent_key_id) AS label,
			count(*) AS findings,
			count(*) FILTER (WHERE f.severity = 'high') AS high,
			(SELECT count(*) FROM analysis_flows fl WHERE fl.project_id = f.project_id
				AND fl.agent_key_id = c.agent_key_id AND fl.created_at >= ?) AS flows,
			max(f.created_at) AS last_at
		FROM analysis_findings f
		JOIN calls c ON c.id = f.call_id
		LEFT JOIN agent_keys k ON k.id = c.agent_key_id
		WHERE f.project_id = ? AND f.created_at >= ?
		GROUP BY c.agent_key_id, k.label, f.project_id
		ORDER BY high DESC, findings DESC LIMIT 50`, since, projectID, since).Scan(ctx, &rows)
	return rows, err
}

// AgentFlows returns the flow edges of one agent key, newest first, for the risk graph.
func (store *Store) AgentFlows(ctx context.Context, projectID, agentKeyID string, since time.Time, limit int) ([]entity.AnalysisFlow, error) {
	flows := []entity.AnalysisFlow{}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	err := store.DB.NewSelect().Model(&flows).
		Where("project_id = ? AND agent_key_id = ? AND created_at >= ?", projectID, agentKeyID, since).
		Order("created_at DESC").Limit(limit).Scan(ctx)
	return flows, err
}

// FindingCounts groups findings for the risk page.
type FindingCount struct {
	Name  string `bun:"name" json:"name"`
	Count int64  `bun:"count" json:"count"`
}

func (store *Store) FindingDistribution(ctx context.Context, projectID string, since time.Time, column string) ([]FindingCount, error) {
	rows := []FindingCount{}
	if column != "code" && column != "source" && column != "mode" && column != "harm" && column != "severity" {
		column = "code"
	}
	err := store.DB.NewRaw(`SELECT `+column+` AS name, count(*) AS count FROM analysis_findings
		WHERE project_id = ? AND created_at >= ? AND `+column+` <> '' GROUP BY 1 ORDER BY count DESC, name LIMIT 50`,
		projectID, since).Scan(ctx, &rows)
	return rows, err
}

// FindingCode is one row of the risk page's code table.
type FindingCode struct {
	Code     string `bun:"code" json:"code"`
	Source   string `bun:"source" json:"source"`
	Mode     string `bun:"mode" json:"mode"`
	Harm     string `bun:"harm" json:"harm"`
	Severity string `bun:"severity" json:"severity"`
	Count    int64  `bun:"count" json:"count"`
}

func (store *Store) FindingCodes(ctx context.Context, projectID string, since time.Time) ([]FindingCode, error) {
	rows := []FindingCode{}
	err := store.DB.NewRaw(`SELECT code, source, mode, harm, severity, count(*) AS count FROM analysis_findings
		WHERE project_id = ? AND created_at >= ? GROUP BY 1,2,3,4,5 ORDER BY count DESC, code LIMIT 50`,
		projectID, since).Scan(ctx, &rows)
	return rows, err
}

// MarkBatchCall records one call once. A retried job is idempotent and cannot count the same call
// twice or close the batch before the other jobs have run.
func (store *Store) MarkBatchCall(ctx context.Context, batchID, callID string, failed bool) error {
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		result, err := transaction.NewRaw(`INSERT INTO analysis_batch_calls (batch_id, call_id, failed, created_at)
			VALUES (?, ?, ?, ?) ON CONFLICT (batch_id, call_id) DO NOTHING`, batchID, callID, failed, time.Now().UTC()).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return nil
		}
		_, err = transaction.NewRaw(`UPDATE analysis_batches SET
				completed = LEAST(completed + CASE WHEN ? THEN 0 ELSE 1 END, total),
				failed = LEAST(failed + CASE WHEN ? THEN 1 ELSE 0 END, total),
				state = CASE WHEN completed + failed + 1 >= total THEN 'completed' ELSE state END,
				finished_at = CASE WHEN completed + failed + 1 >= total THEN ? ELSE finished_at END
			WHERE id = ? AND state = 'running'`, failed, failed, time.Now().UTC(), batchID).Exec(ctx)
		return err
	})
}

func (store *Store) BatchesOfProject(ctx context.Context, projectID string, limit int) ([]entity.AnalysisBatch, error) {
	batches := []entity.AnalysisBatch{}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	err := store.DB.NewSelect().Model(&batches).Where("project_id = ?", projectID).
		Order("created_at DESC").Limit(limit).Scan(ctx)
	return batches, err
}
