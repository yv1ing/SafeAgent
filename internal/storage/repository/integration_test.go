//go:build integration

package repository_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

func TestPrepareCreatesSchemaAndRejectsMismatch(t *testing.T) {
	database := open(t)
	reset(t, database)
	ctx := context.Background()
	if err := database.Prepare(ctx); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(database.Secret) != 32 {
		t.Fatalf("the instance key was not supplied")
	}
	if err := database.Prepare(ctx); err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, `UPDATE system_state SET schema_fingerprint = 'stale' WHERE id = 1`); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if err := database.Prepare(ctx); err == nil {
		t.Fatalf("a mismatched schema fingerprint must stop startup")
	}
}

func TestPrepareRefusesTablesWithoutFingerprint(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	if _, err := database.DB.ExecContext(ctx, `DELETE FROM system_state`); err != nil {
		t.Fatalf("delete fingerprint: %v", err)
	}
	if err := database.Prepare(ctx); err == nil || !strings.Contains(err.Error(), "no schema fingerprint") {
		t.Fatalf("a table set without an anchored fingerprint must be refused: %v", err)
	}
}

func TestAdministratorExistsIgnoresDisabledAccounts(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	if exists, err := database.AdministratorExists(ctx); err != nil || exists {
		t.Fatalf("a fresh database has no administrator: %v %v", exists, err)
	}
	user := &entity.User{ID: "u1", Username: "admin", PasswordHash: "x", Administrator: true, Disabled: true, CreatedAt: time.Now().UTC()}
	if err := database.CreateUser(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if exists, err := database.AdministratorExists(ctx); err != nil || exists {
		t.Fatalf("a disabled administrator must not count: %v %v", exists, err)
	}
}

func TestQuotaRejectsOversizedAndNegativeReservations(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	projectID := seeded.project.ID
	if allowed, _, err := database.ReserveTokens(ctx, projectID, 500, 100); err != nil || allowed {
		t.Fatalf("an estimate above the limit must be rejected: %v %v", allowed, err)
	}
	if _, _, err := database.ReserveTokens(ctx, projectID, -50, 100); err == nil {
		t.Fatalf("a negative estimate must be refused")
	}
	allowed, day, err := database.ReserveTokens(ctx, projectID, 60, 100)
	if err != nil || !allowed {
		t.Fatalf("reservation failed: %v %v", allowed, err)
	}
	if allowed, _, err := database.ReserveTokens(ctx, projectID, 60, 100); err != nil || allowed {
		t.Fatalf("the quota must account for reservations: %v %v", allowed, err)
	}
	if err := database.SettleTokens(ctx, projectID, day, 60, 40); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if allowed, _, err := database.ReserveTokens(ctx, projectID, 30, 100); err != nil || !allowed {
		t.Fatalf("reservation should be released after settlement: %v %v", allowed, err)
	}
}

func TestUnlimitedQuotaStillRecordsUsage(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	allowed, day, err := database.ReserveTokens(ctx, seeded.project.ID, 100, 0)
	if err != nil || !allowed {
		t.Fatalf("an unlimited project must admit the call: %v %v", allowed, err)
	}
	if err := database.SettleTokens(ctx, seeded.project.ID, day, 100, 80); err != nil {
		t.Fatalf("settle: %v", err)
	}
	var used int64
	if err := database.DB.NewRaw(`SELECT used FROM quota_usage WHERE project_id = ? AND day = ?`, seeded.project.ID, day.Format("2006-01-02")).Scan(ctx, &used); err != nil {
		t.Fatalf("unlimited usage was not recorded: %v", err)
	}
	if used != 80 {
		t.Fatalf("expected 80 tokens of recorded usage, got %d", used)
	}
}

func TestDeletingProjectRemovesItsCalls(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	if err := database.CreateCall(ctx, seeded.call("call_1", "s", time.Now().UTC())); err != nil {
		t.Fatalf("create call: %v", err)
	}
	if err := database.DeleteProject(ctx, seeded.project.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := database.Call(ctx, seeded.project.ID, "call_1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("a deleted project must take its calls with it: %v", err)
	}
}

func TestAuxiliaryCallSkipsConversationAndAnalysis(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	call := seeded.auxiliaryCall("aux_1", time.Now().UTC())
	call.SessionID = "client_session"
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatalf("create auxiliary call: %v", err)
	}
	stored, err := database.Call(ctx, seeded.project.ID, call.ID)
	if err != nil {
		t.Fatalf("read call: %v", err)
	}
	if stored.SessionID != "" || stored.AnalysisState != "skipped" {
		t.Fatalf("an auxiliary call must not join a conversation or be diagnosed: %+v", stored)
	}
	if stored.Operation != classify.CountTokens || stored.Lane != classify.LaneAuxiliary || stored.Purpose != classify.PurposeCountTokens {
		t.Fatalf("auxiliary classification was not stored: %+v", stored)
	}
	var conversations int
	if err := database.DB.NewRaw(`SELECT count(*) FROM conversations`).Scan(ctx, &conversations); err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	if conversations != 0 {
		t.Fatalf("an auxiliary call must not create a conversation row, found %d", conversations)
	}
}

func TestSealIsTransactionalAndIsolatesBadRows(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	good := seeded.call("call_1", "s1", time.Now().UTC())
	good.ModelID = "model_1"
	bad := seeded.call("call_2", "s2", time.Now().UTC())
	for _, call := range []*entity.Call{good, bad} {
		if err := database.CreateCall(ctx, call); err != nil {
			t.Fatalf("create call: %v", err)
		}
	}
	if err := database.AppendEvent(&entity.Event{CallID: good.ID, Sequence: 1, ProjectID: seeded.project.ID, Kind: "request", SchemaVersion: auditevent.SchemaVersion, Payload: []byte(`{"body":"ok"}`)}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := database.AppendEvent(&entity.Event{CallID: bad.ID, Sequence: 1, ProjectID: seeded.project.ID, Kind: "request", SchemaVersion: auditevent.SchemaVersion, Payload: []byte("{\"body\":\"bad\\u0000value\"}")}); err != nil {
		t.Fatalf("append: %v", err)
	}
	good.Outcome = classify.OutcomeOK
	if err := database.SealCall(ctx, good, "completed", "", []string{"tool_1"}, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	events, err := database.CallEvents(ctx, seeded.project.ID, good.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("sealed events were not persisted: %v %v", events, err)
	}
	sealed, err := database.Call(ctx, seeded.project.ID, good.ID)
	if err != nil || sealed.State != "completed" || sealed.TerminatedAt == nil || sealed.ModelID != "model_1" {
		t.Fatalf("call was not sealed: %+v %v", sealed, err)
	}
	if sealed.Outcome != classify.OutcomeOK || sealed.UpdatedAt.IsZero() {
		t.Fatalf("the seal must persist the outcome and updated_at: %+v", sealed)
	}
	issuer, err := database.CallByToolCall(ctx, seeded.project.ID, seeded.key.ID, "tool_1")
	if err != nil || issuer.ID != good.ID {
		t.Fatalf("the tool call index was not written with the seal: %+v %v", issuer, err)
	}
}

func TestSealMarksNonMainCallsAsNotAnalyzed(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	side := seeded.sideCall("side", "session", time.Now().UTC())
	side.AnalysisState = "pending"
	if err := database.CreateCall(ctx, side); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := database.SealCall(ctx, side, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	stored, err := database.Call(ctx, seeded.project.ID, side.ID)
	if err != nil || stored.AnalysisState != "skipped" {
		t.Fatalf("a side call must never be queued for diagnosis: %+v %v", stored, err)
	}
}

func TestInterruptRunningMarksLeftoverCalls(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	if err := database.CreateCall(ctx, seeded.call("stale", "s", time.Now().UTC())); err != nil {
		t.Fatalf("create: %v", err)
	}
	count, err := database.InterruptRunning(ctx)
	if err != nil || count != 1 {
		t.Fatalf("running calls were not interrupted: %d %v", count, err)
	}
	call, err := database.Call(ctx, seeded.project.ID, "stale")
	if err != nil || call.State != "interrupted" {
		t.Fatalf("unexpected state: %+v %v", call, err)
	}
}

func TestConversationsPageOnLastActivity(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	for index, session := range []string{"session_a", "session_b"} {
		for turn := range 2 {
			call := seeded.call(session+"_"+strconv.Itoa(turn), session, base.Add(time.Duration(index*10+turn)*time.Minute))
			if err := database.CreateCall(ctx, call); err != nil {
				t.Fatalf("create call: %v", err)
			}
		}
	}
	// An auxiliary call carrying the session header must not appear in the conversation counters.
	auxiliary := seeded.auxiliaryCall("aux", base.Add(20*time.Minute))
	auxiliary.SessionID = "session_b"
	if err := database.CreateCall(ctx, auxiliary); err != nil {
		t.Fatalf("create auxiliary call: %v", err)
	}
	first, err := database.Conversations(ctx, seeded.project.ID, time.Time{}, "", 1)
	if err != nil || len(first) != 1 || first[0].ID != "session_b" || first[0].Calls != 2 {
		t.Fatalf("the most recently active conversation must come first: %+v %v", first, err)
	}
	second, err := database.Conversations(ctx, seeded.project.ID, first[0].LastAt, first[0].ID, 1)
	if err != nil || len(second) != 1 || second[0].ID != "session_a" {
		t.Fatalf("the cursor skipped or repeated a conversation: %+v %v", second, err)
	}
	chain, err := database.SessionCalls(ctx, seeded.project.ID, "session_a", 0)
	if err != nil || len(chain) != 2 || chain[0].ID != "session_a_0" {
		t.Fatalf("session calls must be listed oldest first: %+v %v", chain, err)
	}
	if stored, _ := database.Conversation(ctx, seeded.project.ID, "session_b"); stored == nil || stored.Calls != 2 {
		t.Fatalf("the conversation summary counted an auxiliary call: %+v", stored)
	}
}

func TestCallChainFollowsParents(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	parent := seeded.call("parent", "s", time.Now().UTC().Add(-time.Minute))
	parent.ConversationHash = "conversation"
	child := seeded.call("child", "s", time.Now().UTC())
	child.ParentCallID = parent.ID
	for _, call := range []*entity.Call{parent, child} {
		if err := database.CreateCall(ctx, call); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	found, err := database.CallByConversationHash(ctx, seeded.key.ID, "conversation")
	if err != nil || found.ID != parent.ID {
		t.Fatalf("lookup by conversation hash failed: %+v %v", found, err)
	}
	chain, err := database.CallChain(ctx, child.ID)
	if err != nil || len(chain) != 2 || chain[0].ID != parent.ID {
		t.Fatalf("the chain must list ancestors oldest first: %+v %v", chain, err)
	}
	if _, err := database.CallByResponseID(ctx, seeded.key.ID, "resp_missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("an unknown previous response must map to ErrNotFound, got %v", err)
	}
}

func TestCallFilterByClassification(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	main := seeded.call("main", "session", time.Now().UTC())
	main.Outcome = classify.OutcomeBlocked
	main.Purpose = classify.PurposeChat
	side := seeded.sideCall("side", "session", time.Now().UTC())
	auxiliary := seeded.auxiliaryCall("aux", time.Now().UTC())
	for _, call := range []*entity.Call{main, side, auxiliary} {
		if err := database.CreateCall(ctx, call); err != nil {
			t.Fatalf("create %s: %v", call.ID, err)
		}
		if err := database.SealCall(ctx, call, "completed", "", nil, entity.SealAnalysis{}); err != nil {
			t.Fatalf("seal %s: %v", call.ID, err)
		}
	}
	calls, err := database.Calls(ctx, seeded.project.ID, repository.CallFilter{})
	if err != nil || len(calls) != 2 {
		t.Fatalf("the default listing must hide auxiliary calls: %+v %v", calls, err)
	}
	calls, err = database.Calls(ctx, seeded.project.ID, repository.CallFilter{IncludeAuxiliary: true})
	if err != nil || len(calls) != 3 {
		t.Fatalf("include auxiliary returned %d calls: %v", len(calls), err)
	}
	calls, err = database.Calls(ctx, seeded.project.ID, repository.CallFilter{Lanes: []classify.Lane{classify.LaneAuxiliary}})
	if err != nil || len(calls) != 1 || calls[0].ID != "aux" {
		t.Fatalf("lane filter failed: %+v %v", calls, err)
	}
	calls, err = database.Calls(ctx, seeded.project.ID, repository.CallFilter{Operations: []classify.Operation{classify.CountTokens}, IncludeAuxiliary: true})
	if err != nil || len(calls) != 1 || calls[0].ID != "aux" {
		t.Fatalf("operation filter failed: %+v %v", calls, err)
	}
	calls, err = database.Calls(ctx, seeded.project.ID, repository.CallFilter{Outcomes: []classify.Outcome{classify.OutcomeBlocked}})
	if err != nil || len(calls) != 1 || calls[0].ID != "main" {
		t.Fatalf("outcome filter failed: %+v %v", calls, err)
	}
	calls, err = database.Calls(ctx, seeded.project.ID, repository.CallFilter{Purposes: []classify.Purpose{classify.PurposeCompaction}})
	if err != nil || len(calls) != 1 || calls[0].ID != "side" {
		t.Fatalf("purpose filter failed: %+v %v", calls, err)
	}
}

func TestRevokedKeyCannotAuthenticate(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	if _, err := database.AgentKeyByHash(ctx, seeded.key.Hash); err != nil {
		t.Fatalf("key should authenticate before revocation: %v", err)
	}
	if err := database.RevokeAgentKey(ctx, seeded.project.ID, seeded.key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	revoked, err := database.AgentKey(ctx, seeded.project.ID, seeded.key.ID)
	if err != nil || revoked.RevokedAt == nil || revoked.Enabled {
		t.Fatalf("revocation was not stored: %+v %v", revoked, err)
	}
	if _, err := database.AgentKeyByHash(ctx, seeded.key.Hash); err == nil {
		t.Fatalf("revoked key still authenticates")
	}
}

func TestTouchAgentKeyRecordsUseAtMostOncePerMinute(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	database.TouchAgentKey(ctx, seeded.key.ID)
	touched, err := database.AgentKey(ctx, seeded.project.ID, seeded.key.ID)
	if err != nil || touched.LastUsedAt == nil {
		t.Fatalf("last_used_at was not written: %+v %v", touched, err)
	}
	database.TouchAgentKey(ctx, seeded.key.ID)
	again, err := database.AgentKey(ctx, seeded.project.ID, seeded.key.ID)
	if err != nil || again.LastUsedAt == nil || !again.LastUsedAt.Equal(*touched.LastUsedAt) {
		t.Fatalf("a second touch within a minute must be a no-op: %+v", again)
	}
}

func TestChangePasswordKeepsTheCurrentSession(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	hash, err := secure.HashPassword("Original-Password-1")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := database.CreateUser(ctx, &entity.User{ID: "u1", Username: "alice", PasswordHash: hash, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	current, err := database.Authenticate(ctx, "alice", "Original-Password-1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	other, err := database.Authenticate(ctx, "alice", "Original-Password-1")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if err := database.ChangePassword(ctx, "u1", current.Token, "Original-Password-1", "Replacement-Password-2"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := database.Session(ctx, current.Token); err != nil {
		t.Fatalf("the session that changed the password must stay valid: %v", err)
	}
	if _, err := database.Session(ctx, other.Token); !errors.Is(err, repository.ErrInvalidCredentials) {
		t.Fatalf("other sessions must end: %v", err)
	}
}

func TestMissingRecordsReportNotFound(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	if err := database.DeleteProject(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := database.Call(ctx, "missing", "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown call must map to ErrNotFound, got %v", err)
	}
}

func TestStatisticsAndPrune(t *testing.T) {
	database := prepared(t)
	seeded := seedWithAuxiliary(t, database, 1, 1)
	ctx := context.Background()
	main := seeded.call("call_1", "s", time.Now().UTC())
	main.PublicModel = "m"
	main.ChannelID = "channel_1"
	if err := database.CreateCall(ctx, main); err != nil {
		t.Fatalf("create: %v", err)
	}
	main.InputTokens, main.OutputTokens, main.RedactionValues = 10, 20, 2
	main.FirstTokenMilliseconds, main.DurationMilliseconds = 100, 500
	main.MaxSeverity = "medium"
	main.Currency = "USD"
	main.CostMicros = cost(70)
	main.Outcome = classify.OutcomeOK
	if err := database.SealCall(ctx, main, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	side := seeded.sideCall("call_side", "s", time.Now().UTC())
	side.PublicModel = "m"
	side.InputTokens, side.OutputTokens = 100, 200
	side.Currency = "USD"
	side.CostMicros = cost(30)
	if err := database.CreateCall(ctx, side); err != nil {
		t.Fatalf("create side: %v", err)
	}
	if err := database.SealCall(ctx, side, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal side: %v", err)
	}
	auxiliary := seeded.auxiliaryCall("call_aux", time.Now().UTC())
	auxiliary.InputTokens = 1000
	if err := database.CreateCall(ctx, auxiliary); err != nil {
		t.Fatalf("create auxiliary: %v", err)
	}
	if err := database.SealCall(ctx, auxiliary, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal auxiliary: %v", err)
	}
	statistics, err := database.Statistics(ctx, seeded.project.ID, "24h", "model")
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if statistics.Totals.Calls != 1 || statistics.Totals.InputTokens != 10 || statistics.Totals.FirstTokenP50 != 100 {
		t.Fatalf("statistics must count the main lane only: %+v", statistics.Totals)
	}
	if len(statistics.Groups) != 1 || statistics.Groups[0].Name != "m" || statistics.Groups[0].Calls != 1 {
		t.Fatalf("unexpected groups: %+v", statistics.Groups)
	}
	if len(statistics.Costs) != 1 || statistics.Costs[0].CostMicros != 100 {
		t.Fatalf("costs must include main and side calls: %+v", statistics.Costs)
	}
	summaries, err := database.ProjectSummaries(ctx, []string{seeded.project.ID})
	if err != nil {
		t.Fatalf("project summaries: %v", err)
	}
	summary, found := summaries[seeded.project.ID]
	if !found || summary.Calls24h != 1 || summary.InputTokens24h != 10 || summary.OutputTokens24h != 20 || summary.LastActivityAt == 0 {
		t.Fatalf("unexpected project summary: %+v", summary)
	}
	series, err := database.Series(ctx, seeded.project.ID, 24*time.Hour, time.Hour)
	if err != nil || len(series.Buckets) == 0 {
		t.Fatalf("series: %+v %v", series, err)
	}
	if series.Costs == nil || len(series.Costs) != 1 || series.Costs[0].CostMicros != 100 {
		t.Fatalf("series costs = %+v", series.Costs)
	}
	if _, err := database.DB.ExecContext(ctx, `UPDATE calls SET created_at = now() - interval '3 days'`); err != nil {
		t.Fatalf("age: %v", err)
	}
	if err := database.Prune(ctx, 30); err != nil {
		t.Fatalf("prune: %v", err)
	}
	for _, id := range []string{main.ID, side.ID, auxiliary.ID} {
		if _, err := database.Call(ctx, seeded.project.ID, id); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("pruned call %s still exists: %v", id, err)
		}
	}
}

func TestAuxiliaryRetentionZeroFollowsProjectRetention(t *testing.T) {
	database := prepared(t)
	seeded := seedWithAuxiliary(t, database, 1, 0)
	ctx := context.Background()
	auxiliary := seeded.auxiliaryCall("aux_pruned", time.Now().UTC())
	if err := database.CreateCall(ctx, auxiliary); err != nil {
		t.Fatalf("create auxiliary: %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, `UPDATE calls SET created_at = now() - interval '3 days'`); err != nil {
		t.Fatalf("age: %v", err)
	}
	if err := database.Prune(ctx, 30); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := database.Call(ctx, seeded.project.ID, auxiliary.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("auxiliary retention 0 must follow the project retention: %v", err)
	}
}

func cost(micros int64) *int64 { return &micros }

func TestFailCallEndsOnlyRunningCalls(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	running := seeded.call("running", "s", time.Now().UTC())
	if err := database.CreateCall(ctx, running); err != nil {
		t.Fatalf("create: %v", err)
	}
	running.Outcome = classify.OutcomeUpstreamError
	if err := database.FailCall(ctx, running, "audit_failed"); err != nil {
		t.Fatalf("fail call: %v", err)
	}
	failed, err := database.Call(ctx, seeded.project.ID, running.ID)
	if err != nil || failed.State != "failed" || failed.ErrorCode != "audit_failed" ||
		failed.TerminatedAt == nil || failed.Outcome != classify.OutcomeUpstreamError || failed.UpdatedAt.IsZero() {
		t.Fatalf("a running call must be failed: %+v %v", failed, err)
	}
	if err := database.FailCall(ctx, running, "other"); err != nil {
		t.Fatalf("fail call: %v", err)
	}
	if again, _ := database.Call(ctx, seeded.project.ID, running.ID); again.ErrorCode != "audit_failed" {
		t.Fatalf("a terminal call must not be rewritten: %+v", again)
	}
}

func TestSaveAnalysisIsIdempotentAndProjectsOntoTheCall(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	call := seeded.call("call_1", "s", time.Now().UTC())
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatalf("create call: %v", err)
	}
	if err := database.SealCall(ctx, call, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	now := time.Now().UTC()
	record := &entity.Analysis{
		ID: "an_1", ProjectID: seeded.project.ID, CallID: call.ID, AgentKeyID: seeded.key.ID,
		State: "completed", AdapterVersion: "a", DetectorVersion: "d", CatalogueVersion: "c",
		TaxonomyVersion: "t", InputHash: "hash", ModelState: "disabled", StartedAt: now,
	}
	finding := entity.AnalysisFinding{
		ID: "af_1", AnalysisID: record.ID, ProjectID: seeded.project.ID, CallID: call.ID,
		Fingerprint: "fp", Code: "dangerous_command", Origin: "assistant", Trust: "trusted",
		Sink: "shell_exec", Severity: "high", DetectorID: "content", DetectorVersion: "d",
		FlowSourceCallID: call.ID, FlowSourceSection: "request", FlowSourceOrdinal: 1,
		FlowSourceOrigin: "user", FlowSourcePath: "items.1", FlowRelation: "direct",
		CreatedAt: now,
	}
	flow := entity.AnalysisFlow{
		ID: "fl_1", AnalysisID: record.ID, ProjectID: seeded.project.ID, CallID: call.ID,
		AgentKeyID: seeded.key.ID, SourceCallID: call.ID, SourceSection: "request", SourceOrdinal: 1,
		SourceOrigin: "user", SourcePath: "items.1", Sink: "shell_exec", Relation: "direct",
		ContentHash: "h", Confidence: 0.9, CreatedAt: now,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := database.SaveAnalysis(ctx, record, []entity.AnalysisFinding{finding}, []entity.AnalysisFlow{flow}); err != nil {
			t.Fatalf("save analysis: %v", err)
		}
	}
	stored := []entity.Analysis{}
	if err := database.DB.NewSelect().Model(&stored).Where("call_id = ?", call.ID).Scan(ctx); err != nil {
		t.Fatalf("read analyses: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("the analysis identity must be unique, got %d rows", len(stored))
	}
	projected, err := database.Call(ctx, seeded.project.ID, call.ID)
	if err != nil {
		t.Fatalf("read call: %v", err)
	}
	if projected.LatestAnalysisID != record.ID || projected.FindingCount != 1 || projected.MaxSeverity != "high" {
		t.Fatalf("the call projection was not updated: %+v", projected)
	}
	if projected.UpdatedAt.Before(now) {
		t.Fatalf("saving an analysis must bump updated_at: %+v", projected)
	}
	findings, err := database.FindingsOfCalls(ctx, seeded.project.ID, []string{call.ID})
	if err != nil || len(findings) != 1 || findings[0].FlowSourceCallID != call.ID || findings[0].FlowSourceOrdinal != 1 {
		t.Fatalf("batch findings = %+v, err %v", findings, err)
	}
	flows, err := database.FlowsOfAnalysis(ctx, record.ID)
	if err != nil || len(flows) != 1 || flows[0].SourceSection != "request" || flows[0].SourceOrdinal != 1 {
		t.Fatalf("batch flows = %+v, err %v", flows, err)
	}
	if err := database.Prune(ctx, 30); err != nil {
		t.Fatalf("prune: %v", err)
	}
}

func TestSavePolicyVersionActivatesImmediately(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	for expected := int64(1); expected <= 2; expected++ {
		row := &entity.PolicyVersion{
			ID: "pol_" + strconv.FormatInt(expected, 10), ProjectID: seeded.project.ID,
			Document: []byte(`{"rules":[]}`), Hash: "h", CreatedAt: time.Now().UTC(),
		}
		version, err := database.SavePolicyVersion(ctx, row)
		if err != nil {
			t.Fatalf("save policy: %v", err)
		}
		if version != expected {
			t.Fatalf("expected version %d, got %d", expected, version)
		}
	}
	active, err := database.ActivePolicy(ctx, seeded.project.ID)
	if err != nil || active == nil || active.Version != 2 {
		t.Fatalf("the newest saved version must be active: %+v %v", active, err)
	}
}

func TestSavePolicyVersionReportsAMissingProject(t *testing.T) {
	database := prepared(t)
	_, err := database.SavePolicyVersion(context.Background(), &entity.PolicyVersion{
		ID: "pol_missing", ProjectID: "prj_missing", Document: []byte(`{"rules":[]}`), Hash: "h", CreatedAt: time.Now().UTC(),
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("saving against a missing project must report not found: %v", err)
	}
}

func TestProjectAndChannelUpdatesPersistGatewayDefaults(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	project, err := database.CreateProject(ctx, &entity.Project{Name: "p", RetentionDays: 7}, nil, nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	project.Name = "p2"
	project.DefaultMaxOutputTokens = 4321
	project.StreamReviewBytes = 4096
	project.StreamReviewTimeoutMS = 500
	project.AuxiliaryRetentionDays = 5
	project.DefaultRouteID = "route_default"
	updatedProject, err := database.UpdateProject(ctx, project.ID, *project, project.Version)
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if updatedProject.DefaultMaxOutputTokens != 4321 || updatedProject.StreamReviewBytes != 4096 ||
		updatedProject.StreamReviewTimeoutMS != 500 || updatedProject.AuxiliaryRetentionDays != 5 ||
		updatedProject.DefaultRouteID != "route_default" {
		t.Fatalf("project defaults = %+v", updatedProject)
	}

	seedProvider(t, database, "openai")
	createdModel, err := database.CreateModel(ctx, &entity.Model{
		ProviderID: "openai", UpstreamName: "gpt-5", Kind: entity.ModelGeneration,
		Currency: "USD", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	storedModel, err := database.ModelByProviderName(ctx, "openai", "gpt-5")
	if err != nil || storedModel.Kind != entity.ModelGeneration || storedModel.ID != createdModel.ID {
		t.Fatalf("stored model = %+v, err = %v", storedModel, err)
	}
	channel, err := database.CreateChannel(ctx, &entity.Channel{
		Name: "c", ProviderID: "openai", BaseURL: "https://example.com", Enabled: true,
		Headers: map[string]string{"X-Lab": "one"},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	channel.ForwardHeaders = []string{"x-lab"}
	channel.DefaultMaxOutputTokens = 1234
	updatedChannel, err := database.UpdateChannel(ctx, channel.ID, channel.Channel, channel.Version)
	if err != nil {
		t.Fatalf("update channel: %v", err)
	}
	if updatedChannel.DefaultMaxOutputTokens != 1234 || len(updatedChannel.ForwardHeaders) != 1 || updatedChannel.ForwardHeaders[0] != "x-lab" {
		t.Fatalf("channel defaults were not persisted: %+v", updatedChannel)
	}
	if updatedChannel.ProviderLabel != "openai" || updatedChannel.Version != 2 {
		t.Fatalf("channel record = %+v", updatedChannel)
	}
}

func TestProvidersSeedOnceAndPreserveEdits(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	// Seeding again must not overwrite an operator edit.
	record, err := database.ProviderRecord(ctx, "openai")
	if err != nil {
		t.Fatalf("read provider: %v", err)
	}
	editedEndpoints := record.Endpoints
	updated, err := database.UpdateProvider(ctx, "openai", entity.Provider{Label: "Edited", Endpoints: editedEndpoints, Enabled: true}, record.Version)
	if err != nil {
		t.Fatalf("update provider: %v", err)
	}
	if updated.Label != "Edited" {
		t.Fatalf("provider edit was not stored: %+v", updated)
	}
	seedProvider(t, database, "openai")
	again, err := database.ProviderRecord(ctx, "openai")
	if err != nil || again.Label != "Edited" {
		t.Fatalf("seeding overwrote an operator edit: %+v %v", again, err)
	}
	if _, err := database.CreateChannel(ctx, &entity.Channel{Name: "c", ProviderID: "openai", Enabled: true}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	record, err = database.ProviderRecord(ctx, "openai")
	if err != nil || record.ChannelCount != 1 {
		t.Fatalf("provider channel count = %+v, err %v", record, err)
	}
}

func TestProviderDeleteRefusesBuiltinAndResetsToSeed(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	if err := database.DeleteProvider(ctx, "openai"); !errors.Is(err, repository.ErrBuiltinProvider) {
		t.Fatalf("a built-in provider must not be deleted: %v", err)
	}
	spec := provider.ProviderSpec{
		ID: "openai", Label: "OpenAI",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {Format: model.Chat, BaseURL: "https://api.openai.com", Path: "/v1/chat/completions", Auth: provider.AuthBearer},
		},
	}
	endpoints, err := provider.MarshalEndpoints(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	record, _ := database.ProviderRecord(ctx, "openai")
	if _, err := database.UpdateProvider(ctx, "openai", entity.Provider{Label: "Broken", Endpoints: record.Endpoints, Enabled: false}, record.Version); err != nil {
		t.Fatalf("update provider: %v", err)
	}
	reset, err := database.ResetProvider(ctx, repository.ProviderSeed{ID: "openai", Label: spec.Label, Endpoints: endpoints})
	if err != nil || reset.Label != "OpenAI" || !reset.Enabled {
		t.Fatalf("reset provider = %+v, err %v", reset, err)
	}
	custom, err := database.CreateProvider(ctx, &entity.Provider{Label: "Mine", Endpoints: endpoints, Enabled: true})
	if err != nil || custom.Source != entity.ProviderCustom {
		t.Fatalf("create custom provider = %+v, err %v", custom, err)
	}
	if err := database.DeleteProvider(ctx, custom.ID); err != nil {
		t.Fatalf("delete custom provider: %v", err)
	}
	if _, err := database.Provider(ctx, custom.ID); err != nil {
		t.Fatalf("a soft-deleted provider stays readable for the console: %v", err)
	}
	providers, err := database.Providers(ctx, false)
	if err != nil || len(providers) != 1 {
		t.Fatalf("the deleted provider must be hidden from the default list: %+v %v", providers, err)
	}
}

func TestModelBatchReportsDuplicateName(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	err := database.CreateModels(ctx, []*entity.Model{
		{ProviderID: "openai", UpstreamName: "gpt-5", Kind: entity.ModelGeneration, Currency: "USD", Enabled: true},
		{ProviderID: "openai", UpstreamName: "gpt-5", Kind: entity.ModelGeneration, Currency: "USD", Enabled: true},
	})
	var conflict *repository.ModelNameConflictError
	if !errors.As(err, &conflict) || conflict.UpstreamName != "gpt-5" {
		t.Fatalf("a batch duplicate must name the upstream name: %v", err)
	}
	models, err := database.Models(ctx, repository.ModelFilter{})
	if err != nil || len(models) != 0 {
		t.Fatalf("a failed batch must leave no rows: %+v %v", models, err)
	}
	if err := database.CreateModels(ctx, []*entity.Model{
		{ProviderID: "openai", UpstreamName: "gpt-5", Kind: entity.ModelGeneration, Currency: "USD", Enabled: true},
		{ProviderID: "openai", UpstreamName: "gpt-5-mini", Kind: entity.ModelGeneration, Currency: "USD", Enabled: true},
	}); err != nil {
		t.Fatalf("batch create: %v", err)
	}
	unassociated, err := database.Models(ctx, repository.ModelFilter{Unassociated: true})
	if err != nil || len(unassociated) != 2 {
		t.Fatalf("unassociated models = %+v, err %v", unassociated, err)
	}
}

func TestCapabilityOverrideUpsert(t *testing.T) {
	database := prepared(t)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	catalogModel := seedModel(t, database, "openai", "gpt")
	first := entity.CapabilityOverride{
		ChannelID: channel.ID, ModelID: catalogModel.ID, Format: string(model.Chat), Field: "thinking",
		Value: []byte(`{}`), Reason: "first rejection",
	}
	if err := database.AddCapabilityOverride(ctx, first); err != nil {
		t.Fatalf("add override: %v", err)
	}
	first.Reason = "second rejection"
	if err := database.AddCapabilityOverride(ctx, first); err != nil {
		t.Fatalf("add override again: %v", err)
	}
	overrides, err := database.CapabilityOverrides(ctx, []string{channel.ID})
	if err != nil || len(overrides) != 1 || overrides[0].Reason != "second rejection" {
		t.Fatalf("overrides = %+v, err %v", overrides, err)
	}
	record, err := database.Channel(ctx, channel.ID)
	if err != nil || len(record.CapabilityOverrides) != 1 {
		t.Fatalf("the channel list must carry its overrides: %+v %v", record, err)
	}
}

func TestRouteResolutionAndCandidates(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	createdModel := seedModel(t, database, "openai", "gpt")
	route := seedRoute(t, database, seeded.project.ID, "primary", channel.ID, createdModel.ID)

	resolution, err := database.ResolveRoute(ctx, seeded.project.ID, "primary")
	if err != nil || resolution.Resolution != repository.ResolutionExact || len(resolution.Targets) != 1 {
		t.Fatalf("exact resolution = %+v, err %v", resolution, err)
	}
	if resolution.Targets[0].Model.UpstreamName != "gpt" || resolution.Targets[0].Provider.ID != "openai" ||
		resolution.Targets[0].Channel.ID != channel.ID {
		t.Fatalf("the resolved target must carry channel, model and provider: %+v", resolution.Targets[0])
	}
	if _, err := database.ResolveRoute(ctx, seeded.project.ID, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("an unknown model must be not found: %v", err)
	}

	updated, err := database.UpdateRoute(ctx, seeded.project.ID, route.Route.ID, "renamed",
		[]entity.RouteTarget{{ChannelID: channel.ID, ModelID: createdModel.ID, Weight: 1, Enabled: true}}, nil, true, route.Route.Version)
	if err != nil {
		t.Fatalf("rename route: %v", err)
	}
	resolution, err = database.ResolveRoute(ctx, seeded.project.ID, "primary")
	if err != nil || resolution.Resolution != repository.ResolutionAlias || resolution.Route.ID != updated.Route.ID {
		t.Fatalf("the old name must resolve as a renamed alias: %+v %v", resolution, err)
	}

	if _, err := database.DB.ExecContext(ctx, `UPDATE projects SET default_route_id = ? WHERE id = ?`, updated.Route.ID, seeded.project.ID); err != nil {
		t.Fatalf("set default route: %v", err)
	}
	fallback, err := database.DefaultRoute(ctx, seeded.project.ID)
	if err != nil || fallback.Resolution != repository.ResolutionFallback || fallback.Route.ID != updated.Route.ID {
		t.Fatalf("default route = %+v, err %v", fallback, err)
	}
	if _, err := database.DefaultRoute(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("a project without a default route must be not found: %v", err)
	}

	candidates, err := database.RouteCandidates(ctx, updated.Route.ID)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates = %+v, err %v", candidates, err)
	}
	disabled := false
	if _, err := database.UpdateChannel(ctx, channel.ID, entity.Channel{
		Name: "c", ProviderID: "openai", BaseURL: "https://example.com", Enabled: disabled,
	}, channel.Version); err != nil {
		t.Fatalf("disable channel: %v", err)
	}
	candidates, err = database.RouteCandidates(ctx, updated.Route.ID)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("a disabled channel must leave no candidates: %+v %v", candidates, err)
	}
}

func TestRouteRenameAliasAndDeleteClearsDefault(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	createdModel := seedModel(t, database, "openai", "gpt")
	route := seedRoute(t, database, seeded.project.ID, "old-name", channel.ID, createdModel.ID)
	if _, err := database.DB.ExecContext(ctx, `UPDATE projects SET default_route_id = ? WHERE id = ?`, route.Route.ID, seeded.project.ID); err != nil {
		t.Fatalf("set default: %v", err)
	}
	updated, err := database.UpdateRoute(ctx, seeded.project.ID, route.Route.ID, "new-name",
		[]entity.RouteTarget{{ChannelID: channel.ID, ModelID: createdModel.ID, Weight: 1, Enabled: true}}, []string{"manual-alias"}, true, route.Route.Version)
	if err != nil {
		t.Fatalf("update route: %v", err)
	}
	if len(updated.Aliases) != 2 {
		t.Fatalf("expected the renamed and the manual alias: %+v", updated.Aliases)
	}
	aliases, err := database.RouteByAlias(ctx, seeded.project.ID, "old-name")
	if err != nil || aliases.ID != route.Route.ID {
		t.Fatalf("renamed alias lookup = %+v, err %v", aliases, err)
	}
	if _, err := database.RouteByAlias(ctx, seeded.project.ID, "manual-alias"); err != nil {
		t.Fatalf("manual alias lookup: %v", err)
	}
	if err := database.DeleteRoute(ctx, seeded.project.ID, route.Route.ID); err != nil {
		t.Fatalf("delete route: %v", err)
	}
	remaining := []entity.RouteAlias{}
	if err := database.DB.NewSelect().Model(&remaining).Where("route_id = ?", route.Route.ID).Scan(ctx); err != nil {
		t.Fatalf("read aliases: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("deleting a route must delete its aliases: %+v", remaining)
	}
	project, err := database.Project(ctx, seeded.project.ID)
	if err != nil || project.DefaultRouteID != "" {
		t.Fatalf("deleting the default route must clear the project pointer: %+v %v", project, err)
	}
	if _, err := database.ResolveRoute(ctx, seeded.project.ID, "new-name"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("a deleted route must not resolve: %v", err)
	}
}

func TestChannelSoftDeleteDegradesRouteAndRestore(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	createdModel := seedModel(t, database, "openai", "gpt")
	route := seedRoute(t, database, seeded.project.ID, "primary", channel.ID, createdModel.ID)
	references, err := database.RoutesReferencingChannel(ctx, channel.ID)
	if err != nil || len(references) != 1 || references[0].RouteID != route.Route.ID {
		t.Fatalf("channel references = %+v, err %v", references, err)
	}
	if err := database.DeleteChannel(ctx, channel.ID, false); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	targets, err := database.RouteTargets(ctx, route.Route.ID)
	if err != nil || len(targets) != 1 || targets[0].Target.Enabled {
		t.Fatalf("deleting a channel must disable its targets: %+v %v", targets, err)
	}
	if _, err := database.ResolveRoute(ctx, seeded.project.ID, "primary"); err != nil {
		t.Fatalf("the route stays resolvable but degraded: %v", err)
	}
	if _, err := database.RestoreChannel(ctx, channel.ID); err != nil {
		t.Fatalf("restore channel: %v", err)
	}
	targets, err = database.RouteTargets(ctx, route.Route.ID)
	if err != nil || len(targets) != 1 || !targets[0].Target.Enabled {
		t.Fatalf("restoring a channel must re-enable its targets: %+v %v", targets, err)
	}
}

func TestModelDetachTargets(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	createdModel := seedModel(t, database, "openai", "gpt")
	route := seedRoute(t, database, seeded.project.ID, "primary", channel.ID, createdModel.ID)
	detached, err := database.DetachRouteTargetsOfModel(ctx, createdModel.ID)
	if err != nil || detached != 1 {
		t.Fatalf("detached = %d, err %v", detached, err)
	}
	targets, err := database.RouteTargets(ctx, route.Route.ID)
	if err != nil || len(targets) != 0 {
		t.Fatalf("detached targets = %+v, err %v", targets, err)
	}
	if err := database.DeleteModel(ctx, createdModel.ID, true); err != nil {
		t.Fatalf("delete detached model: %v", err)
	}
	restored, err := database.RestoreModel(ctx, createdModel.ID)
	if err != nil || !restored.Enabled {
		t.Fatalf("restore model = %+v, err %v", restored, err)
	}
}

func TestTrafficSeriesDoesNotDoubleCountRolledBuckets(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	now := time.Now().UTC()
	start := now.Truncate(time.Hour).Add(-2 * time.Hour)
	for index := range 2 {
		call := seeded.call("rollup_"+strconv.Itoa(index), "session", start.Add(-time.Duration(index)*time.Hour))
		call.State = "completed"
		if err := database.CreateCall(ctx, call); err != nil {
			t.Fatalf("create call: %v", err)
		}
	}
	if err := database.Rollup(ctx, now); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	series, err := database.TrafficSeries(ctx, []string{seeded.project.ID}, "1h", now.Add(-6*time.Hour), now, time.Hour)
	if err != nil {
		t.Fatalf("traffic series: %v", err)
	}
	var requests int64
	for _, bucket := range series {
		requests += bucket.Requests
	}
	if requests != 2 {
		t.Fatalf("rolled and live calls must be counted once, got %d in %+v", requests, series)
	}
}

func TestEventsOfCallsReturnTheStoredChain(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	call := seeded.call("summary", "session", time.Now().UTC())
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatalf("create call: %v", err)
	}
	payload := []byte(`{"items":[{"kind":"message","role":"user","parts":[{"kind":"text","text":"hello"}]}],"params":{}}`)
	if err := database.AppendEvent(&entity.Event{CallID: call.ID, Sequence: 1, ProjectID: seeded.project.ID, Kind: "request", SchemaVersion: auditevent.SchemaVersion, Payload: payload}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := database.SealCall(ctx, call, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal call: %v", err)
	}
	all, err := database.EventsOfCalls(ctx, []string{call.ID})
	if err != nil || len(all) != 1 || all[0].Kind != "request" {
		t.Fatalf("all events = %+v, err %v", all, err)
	}
	if !strings.Contains(string(all[0].Payload), `"hello"`) {
		t.Fatalf("the stored request payload must keep its items: %s", all[0].Payload)
	}
	stamp, err := database.SessionEventStamp(ctx, []string{call.ID})
	if err != nil || stamp[call.ID] != 1 {
		t.Fatalf("event stamp = %+v, err %v", stamp, err)
	}
}

func TestAuditChainVerifiesAndLocatesTampering(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	call := seeded.call("call_audit", "session", time.Now().UTC())
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatalf("create call: %v", err)
	}
	previous := ""
	for index, payload := range []string{`{"items":[]}`, `{"state":"completed"}`} {
		kind := auditevent.KindRequest
		if index == 1 {
			kind = auditevent.KindTerminal
		}
		event := &auditevent.ChainEvent{
			CallID: call.ID, Sequence: int64(index + 1), ProjectID: seeded.project.ID,
			Kind: kind, SchemaVersion: auditevent.SchemaVersion, Payload: []byte(payload),
			CreatedAt: time.Now().UTC(), PrevHash: previous,
		}
		if err := auditevent.Sign(event, database.Secret); err != nil {
			t.Fatalf("sign: %v", err)
		}
		previous = event.Hash
		if err := database.AppendEvent(&entity.Event{
			CallID: event.CallID, Sequence: event.Sequence, ProjectID: event.ProjectID,
			Kind: event.Kind, SchemaVersion: event.SchemaVersion, Payload: event.Payload,
			CreatedAt: event.CreatedAt, PrevHash: event.PrevHash, Hash: event.Hash,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	call.AuditHead = previous
	call.AuditEvents = 2
	if err := database.SealCall(ctx, call, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	report, err := database.VerifyCallAudit(ctx, seeded.project.ID, call.ID)
	if err != nil || report.Status != auditevent.StatusVerified {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if _, err := database.DB.ExecContext(ctx, `UPDATE events SET payload = '{"items":[1]}' WHERE call_id = ? AND sequence = 1`, call.ID); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	report, err = database.VerifyCallAudit(ctx, seeded.project.ID, call.ID)
	if err != nil || report.Status != auditevent.StatusBroken || report.BrokenAt != 1 {
		t.Fatalf("tampered report=%+v err=%v", report, err)
	}
}

func TestSealPreservesCancellationAndDeletionTimestamps(t *testing.T) {
	database := prepared(t)
	fixture := seed(t, database, 30)
	ctx := context.Background()
	call := fixture.call("call_seal", "call_seal", time.Now().UTC())
	call.ResponseID = "resp_seal"
	call.ResponseStore = true
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteResponse(ctx, fixture.key.ID, "resp_seal"); err != nil {
		t.Fatal(err)
	}
	database.MarkCallCancelled(ctx, call.ID)
	if err := database.SealCall(ctx, call, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatal(err)
	}
	stored, err := database.Call(ctx, fixture.project.ID, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CancelledAt == nil || stored.ResponseDeletedAt == nil {
		t.Fatalf("seal must not overwrite cancellation or deletion: %+v", stored)
	}
}

func TestScopeCallsNeverReplaysAuxiliary(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	for _, call := range []*entity.Call{seeded.call("main", "session", time.Now().UTC()), seeded.auxiliaryCall("aux", time.Now().UTC())} {
		if err := database.CreateCall(ctx, call); err != nil {
			t.Fatalf("create %s: %v", call.ID, err)
		}
		if err := database.SealCall(ctx, call, "completed", "", nil, entity.SealAnalysis{}); err != nil {
			t.Fatalf("seal %s: %v", call.ID, err)
		}
	}
	calls, err := database.ScopeCalls(ctx, seeded.project.ID, "session", "", "session", "", time.Time{}, time.Time{}, 100)
	if err != nil || len(calls) != 1 || calls[0].ID != "main" {
		t.Fatalf("scope calls = %+v, err %v", calls, err)
	}
}

func TestSealWritesSnapshotDecisionsAndJob(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	now := time.Now().UTC()
	call := seeded.call("call_seal_analysis", "session", now)
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatalf("create call: %v", err)
	}
	jobScope := []byte(`{"call_id":"call_seal_analysis"}`)
	analysis := entity.SealAnalysis{
		Decisions: []entity.Decision{{
			ID: "dec_1", ProjectID: seeded.project.ID, CallID: call.ID, Phase: "request",
			Action: "record", Attributes: []byte(`{ "rule": "r" }`), CreatedAt: now,
		}},
		Snapshot: &entity.RedactionSnapshot{
			ID: "snap_1", CallID: call.ID, Rules: []byte(`[{"label":"email"}]`), CreatedAt: now,
		},
		Job: &entity.AnalysisJob{
			ID: "job_1", ProjectID: seeded.project.ID, Kind: "analyze_call", Scope: jobScope,
			DedupeKey: "call:call_seal_analysis:async",
		},
		RulesVersion: 7,
		RulesHash:    "rules-hash",
	}
	if err := database.SealCall(ctx, call, "completed", "", nil, analysis); err != nil {
		t.Fatalf("seal: %v", err)
	}
	snapshot, err := database.SnapshotOfCall(ctx, call.ID)
	if err != nil || snapshot == nil || snapshot.ID != "snap_1" {
		t.Fatalf("snapshot = %+v, err %v", snapshot, err)
	}
	var rulesVersion int64
	var rulesHash string
	if err := database.DB.NewRaw(`SELECT rules_version, rules_hash FROM redaction_snapshots WHERE id = 'snap_1'`).
		Scan(ctx, &rulesVersion, &rulesHash); err != nil || rulesVersion != 7 || rulesHash != "rules-hash" {
		t.Fatalf("snapshot rule identity = %d %q, err %v", rulesVersion, rulesHash, err)
	}
	decisions, err := database.DecisionsOfCall(ctx, call.ID)
	if err != nil || len(decisions) != 1 || decisions[0].ID != "dec_1" {
		t.Fatalf("decisions = %+v, err %v", decisions, err)
	}
	var jobState string
	if err := database.DB.NewRaw(`SELECT state FROM analysis_jobs WHERE id = 'job_1'`).Scan(ctx, &jobState); err != nil || jobState != "pending" {
		t.Fatalf("job state = %q, err %v", jobState, err)
	}
}

func TestInterruptRunningContinuesAPartiallyFlushedChain(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	call := seeded.call("partial", "s", time.Now().UTC())
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatalf("create call: %v", err)
	}
	previous := ""
	for index := range []string{`{"items":[]}`, `{"items":[]}`} {
		event := &auditevent.ChainEvent{
			CallID: call.ID, Sequence: int64(index + 1), ProjectID: seeded.project.ID,
			Kind: auditevent.KindRequest, SchemaVersion: auditevent.SchemaVersion,
			Payload: []byte(`{"items":[]}`), CreatedAt: time.Now().UTC(), PrevHash: previous,
		}
		if err := auditevent.Sign(event, database.Secret); err != nil {
			t.Fatalf("sign: %v", err)
		}
		previous = event.Hash
		if err := database.AppendEvent(&entity.Event{
			CallID: event.CallID, Sequence: event.Sequence, ProjectID: event.ProjectID,
			Kind: event.Kind, SchemaVersion: event.SchemaVersion, Payload: event.Payload,
			CreatedAt: event.CreatedAt, PrevHash: event.PrevHash, Hash: event.Hash,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	database.FlushEvents()
	partial, err := database.Call(ctx, seeded.project.ID, call.ID)
	if err != nil || partial.AuditEvents != 0 || partial.AuditHead != "" {
		t.Fatalf("only the seal writes the audit anchor: %+v %v", partial, err)
	}
	count, err := database.InterruptRunning(ctx)
	if err != nil || count != 1 {
		t.Fatalf("interrupting a call with flushed events must succeed: count=%d err=%v", count, err)
	}
	interrupted, err := database.Call(ctx, seeded.project.ID, call.ID)
	if err != nil || interrupted.State != "interrupted" || interrupted.AuditEvents != 3 {
		t.Fatalf("the call was not interrupted: %+v %v", interrupted, err)
	}
	events, err := database.CallEvents(ctx, seeded.project.ID, call.ID)
	if err != nil || len(events) != 3 {
		t.Fatalf("events = %+v, err %v", events, err)
	}
	terminal := events[len(events)-1]
	if terminal.Kind != auditevent.KindTerminal || terminal.Sequence != 3 || terminal.PrevHash != previous {
		t.Fatalf("the terminal event must continue the stored chain: %+v", terminal)
	}
	if interrupted.AuditHead != terminal.Hash || interrupted.AuditEvents != terminal.Sequence {
		t.Fatalf("the anchor must name the new chain head: %+v", interrupted)
	}
	decoded, err := auditevent.Decode(terminal.Kind, terminal.Payload)
	if err != nil || decoded.Terminal == nil || decoded.Terminal.State != "interrupted" {
		t.Fatalf("terminal payload = %+v, err %v", decoded.Terminal, err)
	}
	report, err := database.VerifyCallAudit(ctx, seeded.project.ID, call.ID)
	if err != nil || report.Status != auditevent.StatusVerified {
		t.Fatalf("an interrupted call must still verify as a complete chain: %+v err %v", report, err)
	}
}

func TestFindingChartsCountOnlyTheLatestAnalysis(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	now := time.Now().UTC()
	call := seeded.call("replayed", "session", now)
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatalf("create call: %v", err)
	}
	if err := database.SealCall(ctx, call, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	save := func(id, code, severity string, started time.Time) {
		record := &entity.Analysis{
			ID: id, ProjectID: seeded.project.ID, CallID: call.ID, AgentKeyID: seeded.key.ID,
			State: "completed", AdapterVersion: "a", DetectorVersion: "d", CatalogueVersion: "c",
			TaxonomyVersion: "t", InputHash: id, ModelState: "disabled", StartedAt: started,
		}
		finding := entity.AnalysisFinding{
			ID: "af_" + id, AnalysisID: id, ProjectID: seeded.project.ID, CallID: call.ID,
			Fingerprint: "fp_" + id, Code: code, Origin: "assistant", Trust: "trusted",
			Sink: "shell_exec", Severity: severity, DetectorID: "content", DetectorVersion: "d", CreatedAt: now,
		}
		flow := entity.AnalysisFlow{
			ID: "fl_" + id, AnalysisID: id, ProjectID: seeded.project.ID, CallID: call.ID,
			AgentKeyID: seeded.key.ID, SourceOrigin: "user", Sink: "shell_exec",
			Relation: "direct", ContentHash: "h_" + id, Confidence: 0.9, CreatedAt: now,
		}
		if err := database.SaveAnalysis(ctx, record, []entity.AnalysisFinding{finding}, []entity.AnalysisFlow{flow}); err != nil {
			t.Fatalf("save analysis %s: %v", id, err)
		}
	}
	save("an_old", "dangerous_command", "high", now.Add(-time.Hour))
	save("an_new", "pii_leak", "medium", now)
	projected, err := database.Call(ctx, seeded.project.ID, call.ID)
	if err != nil || projected.LatestAnalysisID != "an_new" {
		t.Fatalf("the newest analysis must project onto the call: %+v %v", projected, err)
	}
	series, err := database.Series(ctx, seeded.project.ID, 24*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("series: %v", err)
	}
	var severities int64
	for _, entry := range series.Findings {
		severities += entry.Count
	}
	if severities != 1 {
		t.Fatalf("the severity chart must count only the latest analysis: %+v", series.Findings)
	}
	if len(series.FindingCodes) != 1 || series.FindingCodes[0].Code != "pii_leak" || series.FindingCodes[0].Count != 1 {
		t.Fatalf("the code chart must count only the latest analysis: %+v", series.FindingCodes)
	}
	since := now.Add(-24 * time.Hour)
	risk, err := database.AgentRisk(ctx, seeded.project.ID, since)
	if err != nil || len(risk) != 1 || risk[0].Findings != 1 || risk[0].High != 0 || risk[0].Flows != 1 {
		t.Fatalf("the risk chart must count only the latest analysis: %+v err %v", risk, err)
	}
	distribution, err := database.FindingDistribution(ctx, seeded.project.ID, since, "code")
	if err != nil || len(distribution) != 1 || distribution[0].Name != "pii_leak" || distribution[0].Count != 1 {
		t.Fatalf("the finding distribution must count only the latest analysis: %+v err %v", distribution, err)
	}
	codes, err := database.FindingCodes(ctx, seeded.project.ID, since)
	if err != nil || len(codes) != 1 || codes[0].Code != "pii_leak" || codes[0].Count != 1 {
		t.Fatalf("the finding code table must count only the latest analysis: %+v err %v", codes, err)
	}
}

func TestHeartbeatJobReportsALostLease(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	now := time.Now().UTC()
	batch := &entity.AnalysisBatch{ID: "batch_1", ProjectID: seeded.project.ID, Kind: "replay", State: "running", Total: 1, CreatedAt: now}
	job := &entity.AnalysisJob{
		ID: "job_lease", ProjectID: seeded.project.ID, Kind: "analyze_call",
		Scope: []byte(`{"call_id":"call_lease"}`), State: "pending", MaxAttempts: 5,
		RunAfter: now, DedupeKey: "lease", CreatedAt: now, UpdatedAt: now,
	}
	if err := database.CreateBatchWithJobs(ctx, batch, []*entity.AnalysisJob{job}); err != nil {
		t.Fatalf("create batch: %v", err)
	}
	claimed, err := database.ClaimJobs(ctx, "worker_a", 1, time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claim = %+v, err %v", claimed, err)
	}
	if err := database.HeartbeatJob(ctx, job.ID, "worker_a", time.Minute); err != nil {
		t.Fatalf("a live lease must be extendable: %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, `UPDATE analysis_jobs SET lease_expires_at = now() - interval '1 second' WHERE id = ?`, job.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if recovered, err := database.RecoverExpiredJobs(ctx); err != nil || recovered != 1 {
		t.Fatalf("recovered = %d, err %v", recovered, err)
	}
	if err := database.HeartbeatJob(ctx, job.ID, "worker_a", time.Minute); !errors.Is(err, repository.ErrLeaseLost) {
		t.Fatalf("a requeued lease must be reported as lost, got %v", err)
	}
	claimed, err = database.ClaimJobs(ctx, "worker_b", 1, time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].LockedBy != "worker_b" {
		t.Fatalf("the job must be claimable by another worker: %+v err %v", claimed, err)
	}
	if err := database.CompleteJob(ctx, job.ID, "worker_a"); !errors.Is(err, repository.ErrLeaseLost) {
		t.Fatalf("the stale worker must not complete the job, got %v", err)
	}
}

func TestUpdateRouteWritesEnabledWithTheVersionCheck(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	createdModel := seedModel(t, database, "openai", "gpt")
	route := seedRoute(t, database, seeded.project.ID, "primary", channel.ID, createdModel.ID)
	targets := []entity.RouteTarget{{ChannelID: channel.ID, ModelID: createdModel.ID, Weight: 1, Enabled: true}}
	disabled, err := database.UpdateRoute(ctx, seeded.project.ID, route.Route.ID, "primary", targets, nil, false, route.Route.Version)
	if err != nil {
		t.Fatalf("disable route: %v", err)
	}
	if disabled.Route.Enabled || disabled.Route.Version != route.Route.Version+1 {
		t.Fatalf("the enabled flag must be written with the version check: %+v", disabled.Route)
	}
	if _, err := database.UpdateRoute(ctx, seeded.project.ID, route.Route.ID, "primary", targets, nil, true, route.Route.Version); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("a stale edit must conflict: %v", err)
	}
	stored, err := database.Route(ctx, seeded.project.ID, route.Route.ID)
	if err != nil || stored.Route.Enabled {
		t.Fatalf("the stale edit must not re-enable the route: %+v %v", stored, err)
	}
}

func TestChannelDeleteBumpsRouteVersion(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	createdModel := seedModel(t, database, "openai", "gpt")
	route := seedRoute(t, database, seeded.project.ID, "primary", channel.ID, createdModel.ID)
	stale := route.Route.Version
	if err := database.DeleteChannel(ctx, channel.ID, false); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	current, err := database.Route(ctx, seeded.project.ID, route.Route.ID)
	if err != nil {
		t.Fatalf("read route: %v", err)
	}
	if current.Route.Version <= stale {
		t.Fatalf("deleting a channel must advance the route version: %d", current.Route.Version)
	}
	targets := []entity.RouteTarget{{ChannelID: channel.ID, ModelID: createdModel.ID, Weight: 1, Enabled: true}}
	if _, err := database.UpdateRoute(ctx, seeded.project.ID, route.Route.ID, "primary", targets, nil, true, stale); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("a stale route edit must not write the deleted channel target back: %v", err)
	}
	stored, err := database.RouteTargets(ctx, route.Route.ID)
	if err != nil || len(stored) != 1 || stored[0].Target.Enabled {
		t.Fatalf("the target of a deleted channel must stay disabled: %+v err %v", stored, err)
	}
}

func TestModelDeleteBumpsRouteVersion(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	createdModel := seedModel(t, database, "openai", "gpt")
	route := seedRoute(t, database, seeded.project.ID, "primary", channel.ID, createdModel.ID)
	stale := route.Route.Version
	if err := database.DeleteModel(ctx, createdModel.ID, false); err != nil {
		t.Fatalf("delete model: %v", err)
	}
	current, err := database.Route(ctx, seeded.project.ID, route.Route.ID)
	if err != nil {
		t.Fatalf("read route: %v", err)
	}
	if current.Route.Version <= stale {
		t.Fatalf("deleting a model must advance the route version: %d", current.Route.Version)
	}
	targets := []entity.RouteTarget{{ChannelID: channel.ID, ModelID: createdModel.ID, Weight: 1, Enabled: true}}
	if _, err := database.UpdateRoute(ctx, seeded.project.ID, route.Route.ID, "primary", targets, nil, true, stale); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("a stale route edit must not write the deleted model target back: %v", err)
	}
	if _, err := database.RestoreModel(ctx, createdModel.ID); err != nil {
		t.Fatalf("restore model: %v", err)
	}
	restored, err := database.Route(ctx, seeded.project.ID, route.Route.ID)
	if err != nil || restored.Route.Version <= current.Route.Version {
		t.Fatalf("restoring a model must advance the route version: %+v %v", restored.Route, err)
	}
	stored, err := database.RouteTargets(ctx, route.Route.ID)
	if err != nil || len(stored) != 1 || !stored[0].Target.Enabled {
		t.Fatalf("restoring a model must re-enable its targets: %+v err %v", stored, err)
	}
}

func TestUpdateRouteRollsBackEverythingWhenTargetsFail(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	seedProvider(t, database, "openai")
	channel := seedChannel(t, database, "openai", "c")
	createdModel := seedModel(t, database, "openai", "gpt")
	route, err := database.CreateRoute(ctx, &entity.Route{ProjectID: seeded.project.ID, Name: "primary", Enabled: true},
		[]entity.RouteTarget{{ChannelID: channel.ID, ModelID: createdModel.ID, Weight: 1, Enabled: true}}, []string{"manual"})
	if err != nil {
		t.Fatalf("create route: %v", err)
	}
	before, err := database.Route(ctx, seeded.project.ID, route.Route.ID)
	if err != nil {
		t.Fatalf("read route: %v", err)
	}
	if before.Route.Version != 1 || len(before.Targets) != 1 || len(before.Aliases) != 1 {
		t.Fatalf("unexpected baseline route: %+v", before)
	}
	baselineTargetID := before.Targets[0].Target.ID
	failing := []entity.RouteTarget{{ChannelID: "channel_missing", ModelID: createdModel.ID, Weight: 1, Enabled: true}}
	_, err = database.UpdateRoute(ctx, seeded.project.ID, route.Route.ID, "renamed", failing, []string{"replaced"}, false, before.Route.Version)
	if err == nil || errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("a target that violates its foreign key must fail the whole update: %v", err)
	}
	after, err := database.Route(ctx, seeded.project.ID, route.Route.ID)
	if err != nil {
		t.Fatalf("read route after rollback: %v", err)
	}
	if after.Route.Name != "primary" || after.Route.Version != before.Route.Version || !after.Route.Enabled {
		t.Fatalf("the route row must roll back with the failed target write: %+v", after.Route)
	}
	if len(after.Targets) != 1 || after.Targets[0].Target.ID != baselineTargetID ||
		after.Targets[0].Target.ChannelID != channel.ID || !after.Targets[0].Target.Enabled {
		t.Fatalf("the old target set must survive the rollback: %+v", after.Targets)
	}
	if len(after.Aliases) != 1 || after.Aliases[0].Alias != "manual" || after.Aliases[0].Source != entity.AliasManual {
		t.Fatalf("the alias set must survive the rollback: %+v", after.Aliases)
	}
}
