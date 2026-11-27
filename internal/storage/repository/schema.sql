-- SafeAgent trajectory-native schema baseline. This is the single source of DDL truth. There is no
-- migration path: an existing database with a different fingerprint is refused at startup and must
-- be recreated. The instance key is supplied by configuration and is never stored here.

CREATE TABLE IF NOT EXISTS users (
    id text PRIMARY KEY,
    username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    administrator boolean NOT NULL DEFAULT false,
    must_change_password boolean NOT NULL DEFAULT false,
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS projects (
    id text PRIMARY KEY,
    name text NOT NULL,
    retention_days integer NOT NULL DEFAULT 30,
    auxiliary_retention_days integer NOT NULL DEFAULT 3,
    requests_per_minute integer NOT NULL DEFAULT 0,
    concurrency integer NOT NULL DEFAULT 0,
    daily_token_quota bigint NOT NULL DEFAULT 0,
    default_max_output_tokens integer NOT NULL DEFAULT 0,
    stream_review_bytes integer NOT NULL DEFAULT 0,
    stream_review_timeout_ms integer NOT NULL DEFAULT 300,
    coverage_threshold double precision NOT NULL DEFAULT 0.9,
    default_route_id text NOT NULL DEFAULT '',
    rules_version bigint NOT NULL DEFAULT 0,
    active_policy_version bigint NOT NULL DEFAULT 0,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT projects_retention CHECK (retention_days BETWEEN 1 AND 3650),
    CONSTRAINT projects_auxiliary_retention CHECK (auxiliary_retention_days BETWEEN 0 AND 90),
    CONSTRAINT projects_stream_review CHECK (
        stream_review_bytes BETWEEN 0 AND 65536 AND stream_review_timeout_ms BETWEEN 0 AND 5000
    ),
    CONSTRAINT projects_coverage CHECK (coverage_threshold BETWEEN 0 AND 1)
);

CREATE TABLE IF NOT EXISTS project_members (
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX IF NOT EXISTS project_members_user ON project_members (user_id);

CREATE TABLE IF NOT EXISTS agent_keys (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    label text NOT NULL,
    hash text NOT NULL UNIQUE,
    prefix text NOT NULL,
    requests_per_minute integer NOT NULL DEFAULT 0,
    concurrency integer NOT NULL DEFAULT 0,
    trust_level integer NOT NULL DEFAULT 0,
    role text NOT NULL DEFAULT 'agent',
    enabled boolean NOT NULL DEFAULT true,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL,
    last_used_at timestamptz
);
CREATE INDEX IF NOT EXISTS agent_keys_project ON agent_keys (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS providers (
    id text PRIMARY KEY,
    source text NOT NULL,
    label text NOT NULL,
    endpoints jsonb NOT NULL DEFAULT '{}',
    seed_version integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    CONSTRAINT providers_source CHECK (source IN ('builtin','custom'))
);

CREATE TABLE IF NOT EXISTS channels (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES providers(id) ON DELETE RESTRICT,
    name text NOT NULL,
    base_url text NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}',
    forward_headers jsonb NOT NULL DEFAULT '[]',
    default_max_output_tokens integer NOT NULL DEFAULT 0,
    timeout_seconds integer NOT NULL DEFAULT 0,
    first_byte_timeout_seconds integer NOT NULL DEFAULT 0,
    allow_private_network boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS channels_provider_name ON channels (provider_id, name) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS channel_keys (
    id text PRIMARY KEY,
    channel_id text NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    label text NOT NULL DEFAULT '',
    secret text NOT NULL,
    suffix text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true
);
CREATE INDEX IF NOT EXISTS channel_keys_channel ON channel_keys (channel_id);

CREATE TABLE IF NOT EXISTS models (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES providers(id) ON DELETE RESTRICT,
    upstream_name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    kind text NOT NULL,
    input_price bigint NOT NULL DEFAULT 0,
    output_price bigint NOT NULL DEFAULT 0,
    cache_read_price bigint NOT NULL DEFAULT 0,
    cache_write_price bigint NOT NULL DEFAULT 0,
    currency text NOT NULL DEFAULT 'USD',
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    CONSTRAINT models_kind CHECK (kind IN ('generation','embedding','moderation'))
);
CREATE UNIQUE INDEX IF NOT EXISTS models_provider_upstream ON models (provider_id, upstream_name) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS channel_capability_overrides (
    channel_id text NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    model_id text NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    format text NOT NULL,
    field text NOT NULL,
    value jsonb NOT NULL DEFAULT '{}',
    reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    PRIMARY KEY (channel_id, model_id, format, field)
);

CREATE TABLE IF NOT EXISTS model_routes (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS model_routes_project_name ON model_routes (project_id, name) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS route_aliases (
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    alias text NOT NULL,
    route_id text NOT NULL REFERENCES model_routes(id) ON DELETE CASCADE,
    source text NOT NULL DEFAULT 'manual',
    created_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, alias),
    CONSTRAINT route_aliases_source CHECK (source IN ('manual','renamed'))
);
CREATE INDEX IF NOT EXISTS route_aliases_route ON route_aliases (route_id);

CREATE TABLE IF NOT EXISTS route_targets (
    id text PRIMARY KEY,
    route_id text NOT NULL REFERENCES model_routes(id) ON DELETE CASCADE,
    channel_id text NOT NULL REFERENCES channels(id) ON DELETE RESTRICT,
    model_id text NOT NULL REFERENCES models(id) ON DELETE RESTRICT,
    format text NOT NULL DEFAULT '',
    priority integer NOT NULL DEFAULT 0,
    weight integer NOT NULL DEFAULT 1,
    max_output_tokens integer NOT NULL DEFAULT 0,
    enabled boolean NOT NULL DEFAULT true,
    disabled_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    UNIQUE (route_id, channel_id, model_id, format)
);
CREATE INDEX IF NOT EXISTS route_targets_route ON route_targets (route_id);
CREATE INDEX IF NOT EXISTS route_targets_channel ON route_targets (channel_id);
CREATE INDEX IF NOT EXISTS route_targets_model ON route_targets (model_id);

CREATE TABLE IF NOT EXISTS redaction_rules (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind text NOT NULL,
    detector text NOT NULL DEFAULT '',
    pattern text NOT NULL DEFAULT '',
    capture text NOT NULL DEFAULT '',
    label text NOT NULL,
    action text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    CONSTRAINT redaction_rules_action CHECK (action IN ('replace','block','alert'))
);
CREATE INDEX IF NOT EXISTS redaction_rules_project ON redaction_rules (project_id);

CREATE TABLE IF NOT EXISTS policy_versions (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version bigint NOT NULL,
    document jsonb NOT NULL,
    hash text NOT NULL,
    note text NOT NULL DEFAULT '',
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    UNIQUE (project_id, version)
);
CREATE INDEX IF NOT EXISTS policy_versions_project ON policy_versions (project_id, version DESC);

-- Ledger tables. Only trajectory_events is the write model; segments and blobs hold content,
-- while edges, trajectory_states and checkpoints are derived from the event log.
CREATE TABLE IF NOT EXISTS trajectory_events (
    id text PRIMARY KEY,
    trajectory_id text NOT NULL,
    project_id text NOT NULL,
    seq bigint NOT NULL,
    kind text NOT NULL,
    boundary text NOT NULL,
    actor text NOT NULL,
    principal text NOT NULL DEFAULT '',
    call_id text NOT NULL DEFAULT '',
    action_id text NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL,
    inputs jsonb NOT NULL DEFAULT '[]',
    outputs jsonb NOT NULL DEFAULT '[]',
    auth jsonb,
    checks jsonb NOT NULL DEFAULT '[]',
    obligations jsonb NOT NULL DEFAULT '[]',
    snapshot jsonb,
    capture jsonb NOT NULL DEFAULT '{}',
    payload jsonb NOT NULL,
    prev_hash text NOT NULL,
    hash text NOT NULL,
    UNIQUE (trajectory_id, seq)
);
CREATE INDEX IF NOT EXISTS trajectory_events_kind ON trajectory_events (trajectory_id, kind, seq);
CREATE INDEX IF NOT EXISTS trajectory_events_time ON trajectory_events (occurred_at);
CREATE INDEX IF NOT EXISTS trajectory_events_call ON trajectory_events (call_id) WHERE call_id <> '';
CREATE INDEX IF NOT EXISTS trajectory_events_action ON trajectory_events (action_id) WHERE action_id <> '';
CREATE INDEX IF NOT EXISTS trajectory_events_payload ON trajectory_events USING GIN (payload);

CREATE TABLE IF NOT EXISTS blobs (
    hash text PRIMARY KEY,
    bytes bytea NOT NULL,
    media_type text NOT NULL,
    encrypted boolean NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS segments (
    id text NOT NULL,
    trajectory_id text NOT NULL,
    media_type text NOT NULL,
    bytes_ref text NOT NULL,
    size bigint NOT NULL,
    integrity text NOT NULL,
    confidentiality text NOT NULL,
    source_event text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (trajectory_id, id)
);
CREATE INDEX IF NOT EXISTS segments_trajectory ON segments (trajectory_id, created_at);

CREATE TABLE IF NOT EXISTS edges (
    trajectory_id text NOT NULL,
    src text NOT NULL,
    dst text NOT NULL,
    relation text NOT NULL,
    event_id text NOT NULL,
    confidence real NOT NULL DEFAULT 1,
    PRIMARY KEY (trajectory_id, src, dst, relation)
);
CREATE INDEX IF NOT EXISTS edges_dst ON edges (trajectory_id, dst, relation);
CREATE INDEX IF NOT EXISTS edges_src ON edges (trajectory_id, src, relation);

CREATE TABLE IF NOT EXISTS trajectory_states (
    trajectory_id text PRIMARY KEY,
    version bigint NOT NULL,
    state_hash text NOT NULL,
    head_event text NOT NULL,
    project_id text NOT NULL,
    goal text NOT NULL DEFAULT '',
    constraints jsonb NOT NULL DEFAULT '[]',
    labels jsonb NOT NULL DEFAULT '{}',
    capabilities jsonb NOT NULL DEFAULT '[]',
    obligations jsonb NOT NULL DEFAULT '[]',
    risk jsonb NOT NULL DEFAULT '{}',
    coverage real NOT NULL DEFAULT 1,
    state jsonb NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS checkpoints (
    id text PRIMARY KEY,
    trajectory_id text NOT NULL,
    from_seq bigint NOT NULL,
    to_seq bigint NOT NULL,
    merkle_root text NOT NULL,
    signature text NOT NULL,
    prev_hash text NOT NULL,
    witness_ref text,
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS checkpoints_trajectory ON checkpoints (trajectory_id, to_seq DESC);

-- Projections. They are rebuildable from trajectory_events and are never read as the authority for
-- an authorization or audit decision.
CREATE TABLE IF NOT EXISTS trajectory_summaries (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_key_id text NOT NULL,
    session_id text NOT NULL DEFAULT '',
    goal text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'open',
    coverage double precision NOT NULL DEFAULT 1,
    risk double precision NOT NULL DEFAULT 0,
    max_severity text NOT NULL DEFAULT '',
    first_event_at timestamptz NOT NULL,
    last_event_at timestamptz NOT NULL,
    event_count bigint NOT NULL DEFAULT 0,
    call_count bigint NOT NULL DEFAULT 0,
    finding_count bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL,
    ended_at timestamptz,
    CONSTRAINT trajectory_summaries_state CHECK (state IN ('open','ended','interrupted'))
);
CREATE INDEX IF NOT EXISTS trajectory_summaries_project ON trajectory_summaries (project_id, last_event_at DESC, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS trajectory_summaries_session ON trajectory_summaries (project_id, agent_key_id, session_id) WHERE session_id <> '' AND state = 'open';

CREATE TABLE IF NOT EXISTS calls (
    id text PRIMARY KEY,
    trajectory_id text NOT NULL,
    call_seq bigint NOT NULL DEFAULT 0,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_key_id text NOT NULL,
    client_format text NOT NULL DEFAULT '',
    operation text NOT NULL DEFAULT 'generation',
    purpose text NOT NULL DEFAULT 'unknown',
    purpose_source text NOT NULL DEFAULT 'default',
    lane text NOT NULL DEFAULT 'main',
    client_transport text NOT NULL DEFAULT '',
    upstream_format text NOT NULL DEFAULT '',
    public_model text NOT NULL DEFAULT '',
    model_id text NOT NULL DEFAULT '',
    upstream_model text NOT NULL DEFAULT '',
    channel_id text NOT NULL DEFAULT '',
    route_id text NOT NULL DEFAULT '',
    resolution text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'running',
    outcome text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    stream boolean NOT NULL DEFAULT false,
    first_token_milliseconds integer NOT NULL DEFAULT 0,
    duration_milliseconds integer NOT NULL DEFAULT 0,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    cache_read_tokens bigint NOT NULL DEFAULT 0,
    cache_write_tokens bigint NOT NULL DEFAULT 0,
    reasoning_tokens bigint NOT NULL DEFAULT 0,
    usage_known boolean NOT NULL DEFAULT false,
    cost_micros bigint,
    currency text NOT NULL DEFAULT '',
    redaction_values integer NOT NULL DEFAULT 0,
    redaction_labels jsonb NOT NULL DEFAULT '[]',
    redaction_value_ids jsonb NOT NULL DEFAULT '[]',
    max_severity text NOT NULL DEFAULT '',
    losses jsonb NOT NULL DEFAULT '[]',
    repairs jsonb NOT NULL DEFAULT '[]',
    attempts integer NOT NULL DEFAULT 0,
    response_id text NOT NULL DEFAULT '',
    response_store boolean NOT NULL DEFAULT false,
    response_deleted_at timestamptz,
    cancelled_at timestamptz,
    session_id text NOT NULL DEFAULT '',
    parent_call_id text NOT NULL DEFAULT '',
    conversation_hash text NOT NULL DEFAULT '',
    message_offset integer NOT NULL DEFAULT 0,
    tool_call_ids jsonb NOT NULL DEFAULT '[]',
    event_count bigint NOT NULL DEFAULT 0,
    finding_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    terminated_at timestamptz,
    CONSTRAINT calls_operation CHECK (operation IN (
        'generation','count_tokens','models','model','responses_retrieve',
        'responses_input_items','responses_cancel','responses_delete','embeddings','moderations'
    )),
    CONSTRAINT calls_purpose CHECK (purpose IN (
        'chat','subagent','compaction','title','memory','count_tokens','model_list','embedding',
        'moderation','unknown'
    )),
    CONSTRAINT calls_purpose_source CHECK (purpose_source IN ('declared','heuristic','default')),
    CONSTRAINT calls_lane CHECK (lane IN ('main','side','auxiliary')),
    CONSTRAINT calls_state CHECK (state IN ('running','completed','failed','cancelled','interrupted')),
    CONSTRAINT calls_outcome CHECK (outcome IN (
        '', 'ok','blocked','rejected','client_error','upstream_error','unsupported','not_found',
        'rate_limited','cancelled'
    )),
    CONSTRAINT calls_resolution CHECK (resolution IN ('','exact','alias','explicit','fallback'))
);
CREATE INDEX IF NOT EXISTS calls_project_created ON calls (project_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS calls_project_lane_created ON calls (project_id, lane, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS calls_project_operation ON calls (project_id, operation, created_at DESC);
CREATE INDEX IF NOT EXISTS calls_agent ON calls (agent_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS calls_trajectory ON calls (trajectory_id, call_seq, id);
CREATE INDEX IF NOT EXISTS calls_agent_conversation ON calls (agent_key_id, conversation_hash) WHERE conversation_hash <> '';
CREATE INDEX IF NOT EXISTS calls_agent_response ON calls (agent_key_id, response_id) WHERE response_id <> '';
CREATE INDEX IF NOT EXISTS calls_session ON calls (project_id, session_id, created_at, id) WHERE session_id <> '' AND state = 'open';
CREATE INDEX IF NOT EXISTS calls_project_severity ON calls (project_id, max_severity, created_at DESC) WHERE max_severity <> '';
CREATE INDEX IF NOT EXISTS calls_tool_calls ON calls USING GIN (tool_call_ids);

CREATE TABLE IF NOT EXISTS findings (
    id text PRIMARY KEY,
    project_id text NOT NULL,
    trajectory_id text NOT NULL,
    event_id text NOT NULL,
    call_id text NOT NULL DEFAULT '',
    fingerprint text NOT NULL,
    code text NOT NULL,
    origin text NOT NULL DEFAULT '',
    trust text NOT NULL DEFAULT '',
    sink text NOT NULL DEFAULT '',
    severity text NOT NULL DEFAULT 'info',
    confidence double precision NOT NULL DEFAULT 0,
    detector_id text NOT NULL DEFAULT '',
    detector_version text NOT NULL DEFAULT '',
    evidence jsonb NOT NULL DEFAULT '[]',
    attribution jsonb NOT NULL DEFAULT '[]',
    invariant text NOT NULL DEFAULT '',
    transfer text NOT NULL DEFAULT '',
    control text NOT NULL DEFAULT '',
    segments jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL,
    UNIQUE (trajectory_id, event_id, fingerprint)
);
CREATE INDEX IF NOT EXISTS findings_project ON findings (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS findings_trajectory ON findings (trajectory_id, event_id);
CREATE INDEX IF NOT EXISTS findings_call ON findings (call_id) WHERE call_id <> '';

CREATE TABLE IF NOT EXISTS decision_model_configs (
    project_id text PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    enabled boolean NOT NULL DEFAULT false,
    kind text NOT NULL,
    endpoint text NOT NULL DEFAULT '',
    model text NOT NULL,
    api_key text NOT NULL DEFAULT '',
    timeout_seconds integer NOT NULL DEFAULT 20,
    allow_private boolean NOT NULL DEFAULT false,
    use_raw boolean NOT NULL DEFAULT false,
    daily_token_budget bigint NOT NULL DEFAULT 0,
    authorize_below double precision NOT NULL DEFAULT 0.15,
    updated_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS decision_model_usage (
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    day text NOT NULL,
    tokens bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, day)
);

CREATE TABLE IF NOT EXISTS traffic_rollups (
    bucket_start timestamptz NOT NULL,
    bucket_size text NOT NULL,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    channel_id text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    calls bigint NOT NULL DEFAULT 0,
    errors bigint NOT NULL DEFAULT 0,
    blocked bigint NOT NULL DEFAULT 0,
    input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens bigint NOT NULL DEFAULT 0,
    duration_p95 bigint NOT NULL DEFAULT 0,
    redaction_values bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_start, bucket_size, project_id, channel_id, model)
);
CREATE INDEX IF NOT EXISTS traffic_rollups_project ON traffic_rollups (project_id, bucket_size, bucket_start DESC);

CREATE TABLE IF NOT EXISTS operation_logs (
    id text PRIMARY KEY,
    user_id text REFERENCES users(id) ON DELETE SET NULL,
    username text NOT NULL DEFAULT '',
    action text NOT NULL,
    project_id text NOT NULL DEFAULT '',
    detail jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS operation_logs_created ON operation_logs (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS operation_logs_project ON operation_logs (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS quota_usage (
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    day text NOT NULL,
    used bigint NOT NULL DEFAULT 0,
    reserved bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, day)
);

CREATE TABLE IF NOT EXISTS system_state (
    id integer PRIMARY KEY,
    schema_fingerprint text NOT NULL
);
