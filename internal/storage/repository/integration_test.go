//go:build integration

package repository_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

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
		t.Fatalf("HMAC secret was not loaded")
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

func TestSealIsTransactionalAndIsolatesBadRows(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	good := seeded.call("call_1", "s1", time.Now().UTC())
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
	if err := database.SealCall(ctx, good, "completed", "", []string{"tool_1"}, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	events, err := database.CallEvents(ctx, seeded.project.ID, good.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("sealed events were not persisted: %v %v", events, err)
	}
	sealed, err := database.Call(ctx, seeded.project.ID, good.ID)
	if err != nil || sealed.State != "completed" || sealed.TerminatedAt == nil {
		t.Fatalf("call was not sealed: %+v %v", sealed, err)
	}
	issuer, err := database.CallByToolCall(ctx, seeded.project.ID, seeded.key.ID, "tool_1")
	if err != nil || issuer.ID != good.ID {
		t.Fatalf("the tool call index was not written with the seal: %+v %v", issuer, err)
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
	seeded := seed(t, database, 1)
	ctx := context.Background()
	call := seeded.call("call_1", "s", time.Now().UTC())
	call.PublicModel = "m"
	if err := database.CreateCall(ctx, call); err != nil {
		t.Fatalf("create: %v", err)
	}
	call.InputTokens, call.OutputTokens, call.RedactionHits = 10, 20, 2
	call.FirstTokenMilliseconds, call.DurationMilliseconds = 100, 500
	call.MaxSeverity = "medium"
	call.Currency = "USD"
	if err := database.SealCall(ctx, call, "completed", "", nil, entity.SealAnalysis{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	statistics, err := database.Statistics(ctx, seeded.project.ID, "24h", "model")
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if statistics.Totals.Calls != 1 || statistics.Totals.InputTokens != 10 || statistics.Totals.FirstTokenP50 != 100 {
		t.Fatalf("unexpected statistics: %+v", statistics.Totals)
	}
	if len(statistics.Groups) != 1 || statistics.Groups[0].Name != "m" {
		t.Fatalf("unexpected groups: %+v", statistics.Groups)
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
	if _, err := database.DB.ExecContext(ctx, `UPDATE calls SET created_at = now() - interval '3 days'`); err != nil {
		t.Fatalf("age: %v", err)
	}
	if err := database.Prune(ctx, 30); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := database.Call(ctx, seeded.project.ID, call.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("pruned call still exists: %v", err)
	}
}

func TestFailCallEndsOnlyRunningCalls(t *testing.T) {
	database := prepared(t)
	seeded := seed(t, database, 30)
	ctx := context.Background()
	running := seeded.call("running", "s", time.Now().UTC())
	if err := database.CreateCall(ctx, running); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := database.FailCall(ctx, running.ID, "audit_failed"); err != nil {
		t.Fatalf("fail call: %v", err)
	}
	failed, err := database.Call(ctx, seeded.project.ID, running.ID)
	if err != nil || failed.State != "failed" || failed.ErrorCode != "audit_failed" || failed.TerminatedAt == nil {
		t.Fatalf("a running call must be failed: %+v %v", failed, err)
	}
	if err := database.FailCall(ctx, running.ID, "other"); err != nil {
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
		CreatedAt: now,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := database.SaveAnalysis(ctx, record, []entity.AnalysisFinding{finding}, nil); err != nil {
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
