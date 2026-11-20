package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis/decision"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// Repository is the persistence the pipeline needs. It is an interface so the gateway can be
// driven against an in-memory store and a fake upstream without a database.
type Repository interface {
	EventWriter
	AgentKeyByHash(ctx context.Context, hash string) (*entity.AgentKey, error)
	TouchAgentKey(ctx context.Context, id string)
	Project(ctx context.Context, id string) (*entity.Project, error)
	Rules(ctx context.Context, projectID string) ([]entity.Rule, error)
	ActivePolicy(ctx context.Context, projectID string) (*entity.PolicyVersion, error)
	RouteByName(ctx context.Context, projectID, name string) (*entity.Route, error)
	Routes(ctx context.Context, projectID string) ([]entity.Route, error)
	Channels(ctx context.Context) ([]entity.Channel, error)
	EnabledChannelKeys(ctx context.Context) ([]entity.ChannelKey, error)
	ReserveTokens(ctx context.Context, projectID string, estimate, limit int64) (bool, time.Time, error)
	SettleTokens(ctx context.Context, projectID string, day time.Time, estimate, actual int64) error
	CallByResponseID(ctx context.Context, agentKeyID, responseID string) (*entity.Call, error)
	CallByToolCall(ctx context.Context, projectID, agentKeyID, toolCallID string) (*entity.Call, error)
	CallByConversationHash(ctx context.Context, agentKeyID, hash string) (*entity.Call, error)
	CallChain(ctx context.Context, callID string) ([]entity.Call, error)
	EventsOfKinds(ctx context.Context, callIDs []string, kinds ...string) ([]entity.Event, error)
}

type Identity struct {
	Project  *entity.Project
	AgentKey *entity.AgentKey
}

type Settings struct {
	MaxRequestBytes  int64
	MaxResponseBytes int64
	MaxStreamBytes   int64
	StreamIdle       time.Duration
	ToolHoldBytes    int
	MaxAttempts      int
}

// Veto is the optional decision-model opinion on a side-effecting tool call. It can only add a
// block: the deterministic policy has already allowed the call by the time it is asked.
type Veto interface {
	Authorize(ctx context.Context, trace *observation.Trace, tool observation.ToolCall) (decision.Answer, error)
	Threshold(ctx context.Context, projectID string) float64
}

// Writer is the transport the pipeline streams into; the HTTP layer adapts a response writer to it.
type Writer interface {
	Header() http.Header
	WriteHeader(status int)
	Write(data []byte) (int, error)
	Flush()
}
