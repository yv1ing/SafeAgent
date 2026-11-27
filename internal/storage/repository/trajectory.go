package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/evidence"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/state"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/threat"
	"github.com/yv1ing/SafeAgent/internal/trajectory"
)

var ErrTrajectoryNotFound = errors.New("the trajectory does not exist")

var ErrApprovalNotPending = errors.New("the approval is not pending")

type AppendResult struct {
	Summary    entity.TrajectorySummary
	Events     int
	Checkpoint *entity.Checkpoint
}

// EnsureTrajectory returns the open trajectory of a session or creates one. An empty session
// always creates a fresh trajectory.
func (store *Store) EnsureTrajectory(ctx context.Context, projectID, agentKeyID, sessionID, goal string, threshold float64) (*entity.TrajectorySummary, error) {
	if sessionID != "" {
		existing := new(entity.TrajectorySummary)
		err := store.DB.NewSelect().Model(existing).Where(
			"project_id = ? AND agent_key_id = ? AND session_id = ? AND state = 'open'",
			projectID, agentKeyID, sessionID,
		).Scan(ctx)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	now := time.Now().UTC()
	summary := &entity.TrajectorySummary{
		ID: trajectory.NewID("trj"), ProjectID: projectID, AgentKeyID: agentKeyID,
		SessionID: sessionID, Goal: goal, State: "open", Coverage: 1,
		FirstEventAt: now, LastEventAt: now, CreatedAt: now,
	}
	if sessionID != "" {
		_, err := store.DB.NewInsert().Model(summary).On("CONFLICT DO NOTHING").Exec(ctx)
		if err != nil {
			return nil, err
		}
		if summary.ID != "" {
			existing := new(entity.TrajectorySummary)
			if err := store.DB.NewSelect().Model(existing).Where(
				"project_id = ? AND agent_key_id = ? AND session_id = ? AND state = 'open'",
				projectID, agentKeyID, sessionID,
			).Scan(ctx); err == nil {
				return existing, nil
			}
		}
	}
	if _, err := store.DB.NewInsert().Model(summary).Exec(ctx); err != nil {
		if sessionID == "" {
			return nil, err
		}
		existing := new(entity.TrajectorySummary)
		if scanErr := store.DB.NewSelect().Model(existing).Where(
			"project_id = ? AND agent_key_id = ? AND session_id = ? AND state = 'open'",
			projectID, agentKeyID, sessionID,
		).Scan(ctx); scanErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return summary, nil
}

// Append signs and appends a batch inside one transaction, then applies the projections. An error
// means no event became durable, so a gate can fail closed before releasing an action.
func (store *Store) Append(ctx context.Context, batch trajectory.Batch) (AppendResult, error) {
	if len(batch.Events) == 0 {
		return AppendResult{}, nil
	}
	if batch.TrajectoryID == "" || batch.ProjectID == "" {
		return AppendResult{}, errors.New("a trajectory batch needs a trajectory and a project")
	}
	var result AppendResult
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		summary := new(entity.TrajectorySummary)
		if err := tx.NewSelect().Model(summary).Where("id = ?", batch.TrajectoryID).For("UPDATE").Scan(ctx); err != nil {
			return fmt.Errorf("trajectory %s: %w", batch.TrajectoryID, err)
		}
		if summary.ProjectID != batch.ProjectID {
			return errors.New("the trajectory belongs to another project")
		}
		var last struct {
			Seq  int64  `bun:"seq"`
			Hash string `bun:"hash"`
		}
		err := tx.NewRaw(`SELECT seq, hash FROM trajectory_events WHERE trajectory_id = ? ORDER BY seq DESC LIMIT 1`, batch.TrajectoryID).Scan(ctx, &last)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		previousHash := last.Hash
		if err := store.putContent(ctx, tx, batch); err != nil {
			return err
		}
		current, err := loadTrajectoryState(ctx, tx, batch.TrajectoryID, summary.ProjectID)
		if err != nil {
			return err
		}
		calls := map[string]*entity.Call{}
		findings := make([]threat.Finding, 0)
		edges := make([]trajectory.Edge, 0)
		signed := make([]trajectory.Event, 0, len(batch.Events))
		for _, draft := range batch.Events {
			event, err := draftEvent(batch, draft, last.Seq+int64(len(signed))+1, previousHash, store.Secret)
			if err != nil {
				return err
			}
			if _, err := tx.NewInsert().Model(toEntityEvent(event)).Exec(ctx); err != nil {
				return err
			}
			if err := checkApprovalTransition(current, &event); err != nil {
				return err
			}
			before := current
			after, err := state.Reduce(current, &event)
			if err != nil {
				return fmt.Errorf("reduce %s: %w", event.Kind, err)
			}
			current = after
			findings = append(findings, threat.EvaluateInvariants(before, after, &event)...)
			findings = append(findings, checkedFindings(&event)...)
			call, err := applyCallProjection(ctx, tx, calls, summary, event, after)
			if err != nil {
				return err
			}
			if call != nil {
				calls[call.ID] = call
			}
			edges = append(edges, edgesFor(event)...)
			previousHash = event.Hash
			signed = append(signed, event)
		}
		if err := saveState(ctx, tx, current); err != nil {
			return err
		}
		if err := saveCalls(ctx, tx, calls); err != nil {
			return err
		}
		if err := saveFindings(ctx, tx, summary.ProjectID, signed, findings); err != nil {
			return err
		}
		if err := saveEdges(ctx, tx, edges); err != nil {
			return err
		}
		if err := refreshCallFindings(ctx, tx, callIDs(calls)); err != nil {
			return err
		}
		if err := updateSummary(ctx, tx, summary, current); err != nil {
			return err
		}
		checkpoint, err := saveCheckpoint(ctx, tx, batch.TrajectoryID, store.Secret)
		if err != nil {
			return err
		}
		result = AppendResult{Summary: *summary, Events: len(signed), Checkpoint: checkpoint}
		return nil
	})
	return result, err
}

func (store *Store) AppendHarnessEvents(ctx context.Context, batch trajectory.Batch) error {
	_, err := store.Append(ctx, batch)
	return err
}

func (store *Store) putContent(ctx context.Context, tx bun.Tx, batch trajectory.Batch) error {
	for _, segment := range batch.Segments {
		if segment.TrajectoryID == "" {
			segment.TrajectoryID = batch.TrajectoryID
		}
		row := &entity.Segment{
			ID: segment.ID, TrajectoryID: segment.TrajectoryID, MediaType: segment.MediaType,
			BytesRef: segment.BytesRef, Size: segment.Size, Integrity: segment.Integrity,
			Confidentiality: segment.Confidentiality, SourceEvent: segment.SourceEvent, CreatedAt: segment.CreatedAt,
		}
		if _, err := tx.NewInsert().Model(row).
			On("CONFLICT (trajectory_id, id) DO UPDATE SET integrity = CASE WHEN segments.integrity = 'untrusted' OR EXCLUDED.integrity = 'untrusted' THEN 'untrusted' WHEN segments.integrity = 'unknown' OR EXCLUDED.integrity = 'unknown' THEN 'unknown' ELSE 'trusted' END, confidentiality = CASE WHEN segments.confidentiality = 'user_identity' OR EXCLUDED.confidentiality = 'user_identity' THEN 'user_identity' WHEN segments.confidentiality = 'private' OR EXCLUDED.confidentiality = 'private' THEN 'private' ELSE 'public' END").
			Exec(ctx); err != nil {
			return err
		}
	}
	for _, blob := range batch.Blobs {
		if len(blob.Bytes) == 0 {
			continue
		}
		encrypted := false
		payload := blob.Bytes
		if store.Secret != nil {
			sealed, err := secure.Seal(store.Secret, blob.Bytes)
			if err != nil {
				return err
			}
			payload, encrypted = []byte(sealed), true
		}
		row := &entity.Blob{Hash: blob.Hash, Bytes: payload, MediaType: blob.MediaType, Encrypted: encrypted, CreatedAt: blob.CreatedAt}
		if _, err := tx.NewInsert().Model(row).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func draftEvent(batch trajectory.Batch, draft trajectory.Draft, seq int64, previous string, secret []byte) (trajectory.Event, error) {
	at := draft.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	id := draft.ID
	if id == "" {
		id = trajectory.NewID("evt")
	}
	event := trajectory.Event{
		ID: id, TrajectoryID: batch.TrajectoryID, ProjectID: batch.ProjectID,
		Seq: seq, Kind: draft.Kind, Boundary: draft.Boundary, Actor: draft.Actor,
		Principal: draft.Principal, CallID: draft.CallID, ActionID: draft.ActionID, At: at,
		Inputs: draft.Inputs, Outputs: draft.Outputs, Auth: draft.Auth, Checks: draft.Checks,
		Obligations: draft.Obligations, Snapshot: draft.Snapshot, Capture: draft.Capture,
		Payload: trajectory.Marshal(draft.Payload), PrevHash: previous,
	}
	if event.Kind == "" || event.Boundary == "" || event.Actor == "" {
		return trajectory.Event{}, errors.New("trajectory event is missing kind, boundary or actor")
	}
	if event.Capture.Recorder == "" {
		event.Capture = trajectory.Capture{Recorder: "gateway", Placement: "gateway", Coverage: []trajectory.Boundary{event.Boundary}}
	}
	if err := trajectory.Sign(&event, secret); err != nil {
		return trajectory.Event{}, err
	}
	return event, nil
}

func loadTrajectoryState(ctx context.Context, tx bun.Tx, trajectoryID, projectID string) (*state.State, error) {
	row := new(entity.TrajectoryStateRow)
	err := tx.NewSelect().Model(row).Where("trajectory_id = ?", trajectoryID).Scan(ctx)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return state.New(trajectoryID, projectID), nil
	}
	current := state.New(trajectoryID, projectID)
	if len(row.State) > 0 {
		if err := json.Unmarshal(row.State, current); err != nil {
			return nil, err
		}
	}
	return current, nil
}

func checkApprovalTransition(current *state.State, event *trajectory.Event) error {
	switch event.Kind {
	case trajectory.KindApprovalGranted:
		payload, err := trajectory.DecodePayload[trajectory.ApprovalGranted](event)
		if err != nil {
			return err
		}
		approval, found := current.Approvals[payload.ApprovalID]
		if !found || approval.State != "pending" {
			return ErrApprovalNotPending
		}
		if !payload.ExpiresAt.IsZero() && !payload.ExpiresAt.After(event.At) {
			return ErrApprovalNotPending
		}
	case trajectory.KindApprovalDenied:
		payload, err := trajectory.DecodePayload[trajectory.ApprovalDenied](event)
		if err != nil {
			return err
		}
		approval, found := current.Approvals[payload.ApprovalID]
		if !found || approval.State != "pending" {
			return ErrApprovalNotPending
		}
	}
	return nil
}

func saveState(ctx context.Context, tx bun.Tx, current *state.State) error {
	constraints, _ := json.Marshal(current.Constraints)
	labels, _ := json.Marshal(current.Labels)
	capabilities, _ := json.Marshal(current.Capabilities)
	obligations, _ := json.Marshal(current.Obligations)
	risk, _ := json.Marshal(current.Risk)
	payload, _ := json.Marshal(current)
	row := &entity.TrajectoryStateRow{
		TrajectoryID: current.TrajectoryID, Version: current.Version, StateHash: state.Hash(current),
		HeadEvent: current.HeadEvent, ProjectID: current.ProjectID, Goal: current.Goal,
		Constraints: constraints, Labels: labels, Capabilities: capabilities, Obligations: obligations,
		Risk: risk, Coverage: current.Coverage, State: payload, UpdatedAt: current.UpdatedAt,
	}
	_, err := tx.NewInsert().Model(row).On("CONFLICT (trajectory_id) DO UPDATE").
		Set("version = EXCLUDED.version").Set("state_hash = EXCLUDED.state_hash").
		Set("head_event = EXCLUDED.head_event").Set("project_id = EXCLUDED.project_id").
		Set("goal = EXCLUDED.goal").Set("constraints = EXCLUDED.constraints").Set("labels = EXCLUDED.labels").
		Set("capabilities = EXCLUDED.capabilities").Set("obligations = EXCLUDED.obligations").
		Set("risk = EXCLUDED.risk").Set("coverage = EXCLUDED.coverage").Set("state = EXCLUDED.state").
		Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
	return err
}

func applyCallProjection(ctx context.Context, tx bun.Tx, cache map[string]*entity.Call, summary *entity.TrajectorySummary, event trajectory.Event, current *state.State) (*entity.Call, error) {
	callID := event.CallID
	var payload struct {
		CallID     string
		ActionType trajectory.ActionType
	}
	_ = json.Unmarshal(event.Payload, &payload)
	if callID == "" {
		callID = payload.CallID
	}
	if callID == "" {
		return nil, nil
	}
	call, found := cache[callID]
	if !found {
		call = new(entity.Call)
		err := tx.NewSelect().Model(call).Where("id = ?", callID).Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			call = nil
		}
	}
	switch event.Kind {
	case trajectory.KindActionProposed:
		proposed, err := trajectory.DecodePayload[trajectory.ActionProposed](&event)
		if err != nil {
			return nil, err
		}
		if proposed.ActionType == trajectory.ActionProviderAttempt {
			if call == nil {
				return nil, nil
			}
			call.Attempts++
			call.ChannelID, call.ModelID, call.UpstreamModel = proposed.ChannelID, proposed.ModelID, proposed.UpstreamModel
			call.UpstreamFormat = proposed.Format
			call.UpdatedAt = event.At
			return call, nil
		}
		if call == nil {
			call = &entity.Call{
				ID: proposed.CallID, TrajectoryID: event.TrajectoryID, ProjectID: summary.ProjectID,
				AgentKeyID: event.Principal, ClientFormat: proposed.ClientFormat, Operation: proposed.Operation,
				Purpose: proposed.Purpose, PurposeSource: proposed.PurposeSource, Lane: proposed.Lane,
				PublicModel: proposed.PublicModel, Stream: proposed.Stream, SessionID: proposed.SessionID,
				ParentCallID: proposed.ParentCallID, State: "running", CreatedAt: event.At, UpdatedAt: event.At,
				ToolCallIDs: []string{}, RedactionLabels: []string{}, Losses: []entity.Loss{}, Repairs: []entity.Repair{},
			}
			if call.Operation == "" {
				call.Operation = "generation"
			}
			if call.Purpose == "" {
				call.Purpose = "unknown"
			}
			if call.PurposeSource == "" {
				call.PurposeSource = "default"
			}
			if call.Lane == "" {
				call.Lane = "main"
			}
			if proposed.ActionType == trajectory.ActionModel {
				call.CallSeq = current.NextCallSeq
			} else {
				current.NextCallSeq++
				call.CallSeq = current.NextCallSeq
			}
		}
		if proposed.ActionType == trajectory.ActionTool {
			if proposed.ToolCallID != "" && !contains(call.ToolCallIDs, proposed.ToolCallID) {
				call.ToolCallIDs = append(call.ToolCallIDs, proposed.ToolCallID)
			}
		}
		if proposed.ChannelID != "" {
			call.ChannelID, call.ModelID, call.UpstreamModel = proposed.ChannelID, proposed.ModelID, proposed.UpstreamModel
			call.UpstreamFormat = proposed.Format
		}
		call.EventCount++
		call.UpdatedAt = event.At
		return call, nil
	case trajectory.KindModelChange:
		if call == nil {
			return nil, nil
		}
		change, err := trajectory.DecodePayload[trajectory.ModelChange](&event)
		if err != nil {
			return nil, err
		}
		call.RouteID, call.Resolution = change.RouteID, change.Resolution
		if change.PublicModel != "" {
			call.PublicModel = change.PublicModel
		}
		call.EventCount++
		call.UpdatedAt = event.At
		return call, nil
	case trajectory.KindObservation:
		if call == nil {
			return nil, nil
		}
		observation, err := trajectory.DecodePayload[trajectory.Observation](&event)
		if err != nil {
			return nil, err
		}
		applyHits(call, observation.Redactions)
		call.EventCount++
		call.UpdatedAt = event.At
		return call, nil
	case trajectory.KindActionExecuted:
		executed, err := trajectory.DecodePayload[trajectory.ActionExecuted](&event)
		if err != nil {
			return nil, err
		}
		if call == nil || executed.ActionType != trajectory.ActionModel {
			if call != nil {
				call.EventCount++
			}
			return call, nil
		}
		call.State, call.Outcome, call.ErrorCode = executed.State, executed.Outcome, executed.ErrorCode
		if call.State == "" {
			if executed.ErrorCode == "" {
				call.State, call.Outcome = "completed", "ok"
			} else {
				call.State = "failed"
			}
		}
		call.FirstTokenMilliseconds = executed.FirstTokenMS
		call.DurationMilliseconds = executed.DurationMS
		call.InputTokens = executed.Usage.Input
		call.OutputTokens = executed.Usage.Output
		call.CacheReadTokens = executed.Usage.CacheRead
		call.CacheWriteTokens = executed.Usage.CacheWrite
		call.ReasoningTokens = executed.Usage.Reasoning
		call.UsageKnown = executed.Usage.Known
		call.CostMicros, call.Currency = executed.CostMicros, executed.Currency
		call.ResponseID, call.ResponseStore = executed.ResponseID, executed.ResponseStore
		call.ConversationHash = executed.ConversationHash
		call.Losses, call.Repairs = convertLosses(executed.Losses), convertRepairs(executed.Repairs)
		for _, id := range executed.ToolCallIDs {
			if !contains(call.ToolCallIDs, id) {
				call.ToolCallIDs = append(call.ToolCallIDs, id)
			}
		}
		terminated := event.At
		call.TerminatedAt = &terminated
		call.EventCount++
		call.UpdatedAt = event.At
		return call, nil
	case trajectory.KindGateDecision:
		decision, err := trajectory.DecodePayload[trajectory.GateDecision](&event)
		if err != nil {
			return nil, err
		}
		if call == nil {
			return nil, nil
		}
		call.EventCount++
		if decision.Terminal {
			call.State, call.Outcome, call.ErrorCode = decision.CallState, classify.Outcome(decision.Outcome), decision.ErrorCode
			if call.State == "" {
				call.State = "completed"
			}
			if call.Outcome == "" && decision.Blocked {
				call.Outcome = "blocked"
			}
			if call.ErrorCode == "" {
				call.ErrorCode = decision.Code
			}
			terminated := event.At
			call.TerminatedAt = &terminated
		}
		call.UpdatedAt = event.At
		return call, nil
	default:
		if call != nil {
			call.EventCount++
			call.UpdatedAt = event.At
		}
		return call, nil
	}
}

func saveCalls(ctx context.Context, tx bun.Tx, calls map[string]*entity.Call) error {
	for _, call := range calls {
		_, err := tx.NewInsert().Model(call).On("CONFLICT (id) DO UPDATE").
			Set("trajectory_id = EXCLUDED.trajectory_id").Set("call_seq = EXCLUDED.call_seq").
			Set("state = EXCLUDED.state").Set("outcome = EXCLUDED.outcome").Set("error_code = EXCLUDED.error_code").
			Set("upstream_format = EXCLUDED.upstream_format").Set("model_id = EXCLUDED.model_id").
			Set("upstream_model = EXCLUDED.upstream_model").Set("channel_id = EXCLUDED.channel_id").
			Set("route_id = EXCLUDED.route_id").Set("resolution = EXCLUDED.resolution").
			Set("first_token_milliseconds = EXCLUDED.first_token_milliseconds").Set("duration_milliseconds = EXCLUDED.duration_milliseconds").
			Set("input_tokens = EXCLUDED.input_tokens").Set("output_tokens = EXCLUDED.output_tokens").
			Set("cache_read_tokens = EXCLUDED.cache_read_tokens").Set("cache_write_tokens = EXCLUDED.cache_write_tokens").
			Set("reasoning_tokens = EXCLUDED.reasoning_tokens").Set("usage_known = EXCLUDED.usage_known").
			Set("cost_micros = EXCLUDED.cost_micros").Set("currency = EXCLUDED.currency").
			Set("redaction_values = EXCLUDED.redaction_values").Set("redaction_labels = EXCLUDED.redaction_labels").Set("redaction_value_ids = EXCLUDED.redaction_value_ids").
			Set("max_severity = EXCLUDED.max_severity").Set("losses = EXCLUDED.losses").Set("repairs = EXCLUDED.repairs").
			Set("attempts = EXCLUDED.attempts").Set("response_id = EXCLUDED.response_id").
			Set("response_store = EXCLUDED.response_store").Set("conversation_hash = EXCLUDED.conversation_hash").
			Set("tool_call_ids = EXCLUDED.tool_call_ids").Set("event_count = EXCLUDED.event_count").
			Set("finding_count = EXCLUDED.finding_count").Set("updated_at = EXCLUDED.updated_at").
			Set("terminated_at = EXCLUDED.terminated_at").Exec(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func refreshCallFindings(ctx context.Context, tx bun.Tx, callIDs []string) error {
	if len(callIDs) == 0 {
		return nil
	}
	_, err := tx.NewRaw(`UPDATE calls c SET
			finding_count = COALESCE((SELECT count(*) FROM findings f WHERE f.call_id = c.id), 0),
			max_severity = COALESCE((SELECT CASE max(CASE f.severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END)
				WHEN 3 THEN 'high' WHEN 2 THEN 'medium' WHEN 1 THEN 'info' ELSE '' END
				FROM findings f WHERE f.call_id = c.id), '')
		WHERE c.id IN (?)`, bunIn(callIDs)).Exec(ctx)
	return err
}

func callIDs(calls map[string]*entity.Call) []string {
	ids := make([]string, 0, len(calls))
	for id := range calls {
		ids = append(ids, id)
	}
	return ids
}

func saveFindings(ctx context.Context, tx bun.Tx, projectID string, events []trajectory.Event, findings []threat.Finding) error {
	if len(findings) == 0 {
		return nil
	}
	known := make(map[string]trajectory.Event, len(events))
	for _, event := range events {
		known[event.ID] = event
	}
	for _, finding := range findings {
		event, found := known[finding.EventID]
		if !found {
			continue
		}
		evidencePayload, _ := json.Marshal(finding.Evidence)
		attribution, _ := json.Marshal(finding.Attribution)
		segments, _ := json.Marshal(finding.Segments)
		row := &entity.Finding{
			ID: trajectory.NewID("fnd"), ProjectID: projectID, TrajectoryID: event.TrajectoryID,
			EventID: finding.EventID, CallID: event.CallID, Fingerprint: finding.Fingerprint,
			Code: finding.Code, Origin: string(finding.Origin), Trust: string(finding.Trust), Sink: string(finding.Sink),
			Severity: finding.Severity, Confidence: finding.Confidence, DetectorID: finding.Detector.ID,
			DetectorVersion: finding.Detector.Version, Evidence: evidencePayload, Attribution: attribution,
			Invariant: finding.Invariant, Transfer: finding.Transfer, Control: finding.Control,
			Segments: segments, CreatedAt: time.Now().UTC(),
		}
		if _, err := tx.NewInsert().Model(row).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func saveEdges(ctx context.Context, tx bun.Tx, edges []trajectory.Edge) error {
	for _, edge := range edges {
		row := &entity.Edge{TrajectoryID: edge.TrajectoryID, From: edge.From, To: edge.To, Relation: edge.Relation, EventID: edge.EventID, Confidence: edge.Confidence}
		if _, err := tx.NewInsert().Model(row).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func updateSummary(ctx context.Context, tx bun.Tx, summary *entity.TrajectorySummary, current *state.State) error {
	counts := struct {
		Events   int64     `bun:"events"`
		Last     time.Time `bun:"last"`
		Calls    int64     `bun:"calls"`
		Findings int64     `bun:"findings"`
		Severity string    `bun:"severity"`
	}{}
	if err := tx.NewRaw(`SELECT
			(SELECT count(*) FROM trajectory_events WHERE trajectory_id = ?) AS events,
			(SELECT max(occurred_at) FROM trajectory_events WHERE trajectory_id = ?) AS last,
			(SELECT count(*) FROM calls WHERE trajectory_id = ?) AS calls,
			(SELECT count(*) FROM findings WHERE trajectory_id = ?) AS findings,
			COALESCE((SELECT CASE max(CASE severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'info' THEN 1 ELSE 0 END)
				WHEN 3 THEN 'high' WHEN 2 THEN 'medium' WHEN 1 THEN 'info' ELSE '' END
				FROM findings WHERE trajectory_id = ?), '') AS severity`,
		summary.ID, summary.ID, summary.ID, summary.ID, summary.ID).Scan(ctx, &counts); err != nil {
		return err
	}
	summary.EventCount, summary.CallCount, summary.FindingCount = counts.Events, counts.Calls, counts.Findings
	summary.LastEventAt = counts.Last
	summary.Coverage, summary.Risk = current.Coverage, current.Risk.Score
	summary.MaxSeverity = threat.StrongerSeverity(counts.Severity, current.Risk.MaxSeverity)
	summary.Goal = current.Goal
	if !current.Open {
		if summary.State != "interrupted" {
			summary.State = "ended"
		}
		ended := counts.Last
		summary.EndedAt = &ended
	}
	_, err := tx.NewUpdate().Model(summary).WherePK().
		Set("event_count = ?", summary.EventCount).Set("last_event_at = ?", summary.LastEventAt).
		Set("coverage = ?", summary.Coverage).Set("risk = ?", summary.Risk).
		Set("max_severity = ?", summary.MaxSeverity).Set("finding_count = ?", summary.FindingCount).
		Set("goal = ?", summary.Goal).Set("state = ?", summary.State).Set("ended_at = ?", summary.EndedAt).
		Set("call_count = ?", summary.CallCount).Exec(ctx)
	return err
}

func saveCheckpoint(ctx context.Context, tx bun.Tx, trajectoryID string, secret []byte) (*entity.Checkpoint, error) {
	rows := []entity.TrajectoryEvent{}
	if err := tx.NewSelect().Model(&rows).Where("trajectory_id = ?", trajectoryID).Order("seq ASC").Scan(ctx); err != nil {
		return nil, err
	}
	events := make([]trajectory.Event, 0, len(rows))
	for index := range rows {
		event, err := fromEntityEvent(&rows[index])
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return nil, nil
	}
	previous := new(entity.Checkpoint)
	err := tx.NewSelect().Model(previous).Where("trajectory_id = ?", trajectoryID).Order("to_seq DESC").Limit(1).Scan(ctx)
	var prior *evidence.Checkpoint
	if err == nil {
		prior = &evidence.Checkpoint{ID: previous.ID, TrajectoryID: previous.TrajectoryID, FromSeq: previous.FromSeq, ToSeq: previous.ToSeq, MerkleRoot: previous.MerkleRoot, Signature: previous.Signature, PrevHash: previous.PrevHash, WitnessRef: previous.WitnessRef, CreatedAt: previous.CreatedAt}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	checkpoint := evidence.NewCheckpoint(trajectoryID, events, secret, prior)
	row := &entity.Checkpoint{
		ID: checkpoint.ID, TrajectoryID: checkpoint.TrajectoryID, FromSeq: checkpoint.FromSeq, ToSeq: checkpoint.ToSeq,
		MerkleRoot: checkpoint.MerkleRoot, Signature: checkpoint.Signature, PrevHash: checkpoint.PrevHash,
		WitnessRef: checkpoint.WitnessRef, CreatedAt: checkpoint.CreatedAt,
	}
	if _, err := tx.NewInsert().Model(row).Exec(ctx); err != nil {
		return nil, err
	}
	return row, nil
}

func edgesFor(event trajectory.Event) []trajectory.Edge {
	edges := make([]trajectory.Edge, 0, len(event.Inputs)+len(event.Outputs))
	anchor := event.ActionID
	if anchor == "" {
		anchor = event.ID
	}
	for _, ref := range event.Inputs {
		edges = append(edges, trajectory.Edge{
			TrajectoryID: event.TrajectoryID, From: ref.Segment, To: anchor, Relation: trajectory.RelationUsedData,
			EventID: event.ID, Confidence: 1,
		})
	}
	for _, ref := range event.Outputs {
		edges = append(edges, trajectory.Edge{
			TrajectoryID: event.TrajectoryID, From: anchor, To: ref.Segment, Relation: trajectory.RelationDerivedFrom,
			EventID: event.ID, Confidence: 1,
		})
	}
	if event.Kind == trajectory.KindActionAuthorized && event.ActionID != "" {
		edges = append(edges, trajectory.Edge{
			TrajectoryID: event.TrajectoryID, From: event.ActionID, To: event.ID, Relation: trajectory.RelationAuthorizedBy,
			EventID: event.ID, Confidence: 1,
		})
	}
	return edges
}

func toEntityEvent(event trajectory.Event) *entity.TrajectoryEvent {
	inputs, _ := json.Marshal(event.Inputs)
	outputs, _ := json.Marshal(event.Outputs)
	checks, _ := json.Marshal(event.Checks)
	obligations, _ := json.Marshal(event.Obligations)
	capture, _ := json.Marshal(event.Capture)
	var auth, snapshot json.RawMessage
	if event.Auth != nil {
		auth, _ = json.Marshal(event.Auth)
	}
	if event.Snapshot != nil {
		snapshot, _ = json.Marshal(event.Snapshot)
	}
	return &entity.TrajectoryEvent{
		ID: event.ID, TrajectoryID: event.TrajectoryID, ProjectID: event.ProjectID, Seq: event.Seq,
		Kind: string(event.Kind), Boundary: string(event.Boundary), Actor: string(event.Actor),
		Principal: event.Principal, CallID: event.CallID, ActionID: event.ActionID, OccurredAt: event.At,
		Inputs: inputs, Outputs: outputs, Auth: auth, Checks: checks, Obligations: obligations,
		Snapshot: snapshot, Capture: capture, Payload: event.Payload, PrevHash: event.PrevHash, Hash: event.Hash,
	}
}

func fromEntityEvent(row *entity.TrajectoryEvent) (trajectory.Event, error) {
	event := trajectory.Event{
		ID: row.ID, TrajectoryID: row.TrajectoryID, ProjectID: row.ProjectID, Seq: row.Seq,
		Kind: trajectory.Kind(row.Kind), Boundary: trajectory.Boundary(row.Boundary), Actor: trajectory.Actor(row.Actor),
		Principal: row.Principal, CallID: row.CallID, ActionID: row.ActionID, At: row.OccurredAt,
		Payload: row.Payload, PrevHash: row.PrevHash, Hash: row.Hash,
	}
	if err := json.Unmarshal(row.Inputs, &event.Inputs); err != nil {
		return trajectory.Event{}, err
	}
	if err := json.Unmarshal(row.Outputs, &event.Outputs); err != nil {
		return trajectory.Event{}, err
	}
	if err := json.Unmarshal(row.Checks, &event.Checks); err != nil {
		return trajectory.Event{}, err
	}
	if err := json.Unmarshal(row.Obligations, &event.Obligations); err != nil {
		return trajectory.Event{}, err
	}
	if len(row.Capture) > 0 {
		_ = json.Unmarshal(row.Capture, &event.Capture)
	}
	if len(row.Auth) > 0 {
		event.Auth = new(trajectory.Auth)
		if err := json.Unmarshal(row.Auth, event.Auth); err != nil {
			return trajectory.Event{}, err
		}
	}
	if len(row.Snapshot) > 0 {
		event.Snapshot = new(trajectory.Snapshot)
		if err := json.Unmarshal(row.Snapshot, event.Snapshot); err != nil {
			return trajectory.Event{}, err
		}
	}
	return event, nil
}

func convertLosses(losses []trajectory.Loss) []entity.Loss {
	converted := make([]entity.Loss, 0, len(losses))
	for _, loss := range losses {
		converted = append(converted, entity.Loss{
			Path: loss.Path, Code: loss.Code, Class: loss.Class, Detail: loss.Detail,
			From: loss.From, To: loss.To, ItemKey: loss.ItemKey,
		})
	}
	return converted
}

func convertRepairs(repairs []trajectory.Repair) []entity.Repair {
	converted := make([]entity.Repair, 0, len(repairs))
	for _, repair := range repairs {
		converted = append(converted, entity.Repair{Kind: repair.Kind, Path: repair.Path, Detail: repair.Detail, ItemKey: repair.ItemKey})
	}
	return converted
}

func applyHits(call *entity.Call, hits []trajectory.RedactionHit) {
	labels := map[string]bool{}
	for _, value := range call.RedactionLabels {
		labels[value] = true
	}
	values := map[string]bool{}
	for _, value := range call.RedactionValueIDs {
		values[value] = true
	}
	for _, hit := range hits {
		if hit.ValueID != "" && !values[hit.ValueID] {
			values[hit.ValueID] = true
			call.RedactionValueIDs = append(call.RedactionValueIDs, hit.ValueID)
			call.RedactionValues++
		}
		if hit.Label != "" && !labels[hit.Label] {
			labels[hit.Label] = true
			call.RedactionLabels = append(call.RedactionLabels, hit.Label)
		}
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func checkedFindings(event *trajectory.Event) []threat.Finding {
	if event.Kind != trajectory.KindGateDecision {
		return nil
	}
	decision, err := trajectory.DecodePayload[trajectory.GateDecision](event)
	if err != nil {
		return nil
	}
	findings := make([]threat.Finding, 0, len(decision.Checks))
	for index, check := range decision.Checks {
		if check.Code == "" || check.Status != "finding" {
			continue
		}
		fingerprint := check.ID
		if fingerprint == "" {
			fingerprint = trajectory.ContentHash([]byte(event.ID + check.Code + string(rune(index))))
		}
		control := "record"
		if decision.Blocked {
			control = "block"
		}
		finding := threat.Finding{
			Fingerprint: fingerprint, Code: check.Code, Severity: check.Severity,
			Confidence: check.Confidence, Detector: threat.DetectorRef{ID: "gate", Version: "1"},
			Evidence: []threat.Evidence{{Section: "event", Path: event.ID, View: "event", Quote: check.Detail}},
			EventID:  event.ID, Transfer: string(event.Kind), Control: control,
		}
		if finding.Severity == "" {
			finding.Severity = threat.SeverityInfo
		}
		findings = append(findings, finding)
	}
	return findings
}
