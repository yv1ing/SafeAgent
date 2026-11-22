-- SafeAgent schema baseline. This file is the single source of DDL truth; entities are only CRUD
-- mappings. There is no migration path: a build whose fingerprint differs refuses to start and the
-- operator recreates the database, which is acceptable because the only durable value is the audit
-- trail and this project deliberately keeps no compatibility mechanism.

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
    requests_per_minute integer NOT NULL DEFAULT 0,
    concurrency integer NOT NULL DEFAULT 0,
    daily_token_quota bigint NOT NULL DEFAULT 0,
    default_max_output_tokens integer NOT NULL DEFAULT 0,
    rules_version bigint NOT NULL DEFAULT 0,
    active_policy_version bigint NOT NULL DEFAULT 0,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT projects_retention CHECK (retention_days BETWEEN 1 AND 3650)
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

CREATE TABLE IF NOT EXISTS models (
    id text PRIMARY KEY,
    provider_id text NOT NULL,
    upstream_name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    kind text NOT NULL,
    formats jsonb NOT NULL DEFAULT '[]',
    input_price bigint NOT NULL DEFAULT 0,
    output_price bigint NOT NULL DEFAULT 0,
    cache_read_price bigint NOT NULL DEFAULT 0,
    cache_write_price bigint NOT NULL DEFAULT 0,
    currency text NOT NULL DEFAULT 'USD',
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    UNIQUE (provider_id, upstream_name),
    CONSTRAINT models_kind CHECK (kind IN ('generation','embedding','moderation'))
);

CREATE TABLE IF NOT EXISTS channels (
    id text PRIMARY KEY,
    name text NOT NULL,
    provider_id text NOT NULL,
    endpoints jsonb NOT NULL DEFAULT '[]',
    model_ids jsonb NOT NULL DEFAULT '[]',
    capability_overrides jsonb NOT NULL DEFAULT '[]',
    headers jsonb NOT NULL DEFAULT '{}',
    forward_headers jsonb NOT NULL DEFAULT '[]',
    default_max_output_tokens integer NOT NULL DEFAULT 0,
    timeout_seconds integer NOT NULL DEFAULT 0,
    first_byte_timeout_seconds integer NOT NULL DEFAULT 0,
    allow_private_network boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS channel_keys (
    id text PRIMARY KEY,
    channel_id text NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    label text NOT NULL DEFAULT '',
    secret text NOT NULL,
    suffix text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true
);
CREATE INDEX IF NOT EXISTS channel_keys_channel ON channel_keys (channel_id);

CREATE TABLE IF NOT EXISTS model_routes (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL,
    targets jsonb NOT NULL DEFAULT '[]',
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    UNIQUE (project_id, name)
);

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

CREATE TABLE IF NOT EXISTS calls (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_key_id text NOT NULL REFERENCES agent_keys(id) ON DELETE CASCADE,
    client_format text NOT NULL,
    kind text NOT NULL DEFAULT '',
    client_transport text NOT NULL DEFAULT '',
    endpoint_format text NOT NULL DEFAULT '',
    public_model text NOT NULL DEFAULT '',
    model_id text NOT NULL DEFAULT '',
    upstream_model text NOT NULL DEFAULT '',
    channel_id text NOT NULL DEFAULT '',
    state text NOT NULL,
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
    redaction_hits integer NOT NULL DEFAULT 0,
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
    audit_head text NOT NULL DEFAULT '',
    audit_events bigint NOT NULL DEFAULT 0,
    analysis_state text NOT NULL DEFAULT 'pending',
    latest_analysis_id text NOT NULL DEFAULT '',
    finding_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL,
    terminated_at timestamptz,
    CONSTRAINT calls_state CHECK (state IN ('running','completed','failed','blocked','cancelled','interrupted','rejected')),
    CONSTRAINT calls_analysis_state CHECK (analysis_state IN ('pending','running','completed','failed'))
);
CREATE INDEX IF NOT EXISTS calls_project_created ON calls (project_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS calls_agent ON calls (agent_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS calls_agent_conversation ON calls (agent_key_id, conversation_hash) WHERE conversation_hash <> '';
CREATE INDEX IF NOT EXISTS calls_agent_response ON calls (agent_key_id, response_id) WHERE response_id <> '';
CREATE INDEX IF NOT EXISTS calls_session ON calls (project_id, session_id, created_at, id);
CREATE INDEX IF NOT EXISTS calls_parent ON calls (parent_call_id) WHERE parent_call_id <> '';
CREATE INDEX IF NOT EXISTS calls_project_severity ON calls (project_id, max_severity, created_at DESC) WHERE max_severity <> '';
CREATE INDEX IF NOT EXISTS calls_project_analysis ON calls (project_id, analysis_state, created_at DESC);

CREATE TABLE IF NOT EXISTS redaction_snapshots (
    id text PRIMARY KEY,
    call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    rules jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS redaction_snapshots_call ON redaction_snapshots (call_id);

CREATE TABLE IF NOT EXISTS events (
    call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    sequence bigint NOT NULL,
    project_id text NOT NULL,
    kind text NOT NULL,
    schema_version integer NOT NULL DEFAULT 6,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    prev_hash text NOT NULL DEFAULT '',
    hash text NOT NULL DEFAULT '',
    PRIMARY KEY (call_id, sequence)
);
CREATE INDEX IF NOT EXISTS events_project_kind ON events (project_id, kind, created_at DESC);
CREATE INDEX IF NOT EXISTS events_project_created ON events (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS call_tools (
    project_id text NOT NULL,
    tool_call_id text NOT NULL,
    call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, tool_call_id)
);
CREATE INDEX IF NOT EXISTS call_tools_call ON call_tools (call_id);

CREATE TABLE IF NOT EXISTS conversations (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    first_call_id text NOT NULL,
    last_call_id text NOT NULL,
    created_at timestamptz NOT NULL,
    last_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS conversations_project_last ON conversations (project_id, last_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS analyses (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    agent_key_id text NOT NULL,
    batch_id text NOT NULL DEFAULT '',
    state text NOT NULL,
    adapter_version text NOT NULL,
    detector_version text NOT NULL,
    catalogue_version text NOT NULL,
    taxonomy_version text NOT NULL,
    decision_model text NOT NULL DEFAULT '',
    input_hash text NOT NULL,
    verdict jsonb NOT NULL DEFAULT '{}',
    coverage jsonb NOT NULL DEFAULT '{}',
    model jsonb,
    model_state text NOT NULL DEFAULT 'disabled',
    error text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    CONSTRAINT analyses_state CHECK (state IN ('running','completed','failed')),
    CONSTRAINT analyses_model_state CHECK (model_state IN ('disabled','ok','budget_exhausted','failed'))
);
CREATE INDEX IF NOT EXISTS analyses_call ON analyses (call_id, started_at DESC);
CREATE INDEX IF NOT EXISTS analyses_project ON analyses (project_id, started_at DESC);
CREATE INDEX IF NOT EXISTS analyses_batch ON analyses (batch_id) WHERE batch_id <> '';
CREATE UNIQUE INDEX IF NOT EXISTS analyses_identity ON analyses (call_id, input_hash, detector_version, decision_model);
CREATE INDEX IF NOT EXISTS analyses_finished ON analyses (finished_at);

CREATE TABLE IF NOT EXISTS analysis_findings (
    id text PRIMARY KEY,
    analysis_id text NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    project_id text NOT NULL,
    call_id text NOT NULL,
    fingerprint text NOT NULL,
    code text NOT NULL,
    origin text NOT NULL,
    trust text NOT NULL,
    sink text NOT NULL,
    source text NOT NULL DEFAULT '',
    mode text NOT NULL DEFAULT '',
    harm text NOT NULL DEFAULT '',
    severity text NOT NULL,
    confidence double precision NOT NULL,
    detector_id text NOT NULL,
    detector_version text NOT NULL,
    evidence jsonb NOT NULL DEFAULT '[]',
    attribution jsonb NOT NULL DEFAULT '[]',
    flow_source_turn integer NOT NULL DEFAULT 0,
    flow_source_item integer NOT NULL DEFAULT 0,
    flow_source_origin text NOT NULL DEFAULT '',
    flow_relation text NOT NULL DEFAULT '',
    flow_sink_tool_call_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    UNIQUE (analysis_id, fingerprint)
);
CREATE INDEX IF NOT EXISTS analysis_findings_project ON analysis_findings (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS analysis_findings_code ON analysis_findings (project_id, code, created_at DESC);
CREATE INDEX IF NOT EXISTS analysis_findings_source ON analysis_findings (project_id, source, created_at DESC);

CREATE TABLE IF NOT EXISTS analysis_flows (
    id text PRIMARY KEY,
    analysis_id text NOT NULL REFERENCES analyses(id) ON DELETE CASCADE,
    project_id text NOT NULL,
    call_id text NOT NULL,
    agent_key_id text NOT NULL,
    source_turn integer NOT NULL,
    source_item integer NOT NULL,
    source_origin text NOT NULL,
    source_path text NOT NULL DEFAULT '',
    sink_tool_call_id text NOT NULL DEFAULT '',
    sink text NOT NULL,
    relation text NOT NULL,
    content_hash text NOT NULL,
    confidence double precision NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS analysis_flows_agent ON analysis_flows (project_id, agent_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS analysis_flows_call ON analysis_flows (call_id, created_at);
CREATE INDEX IF NOT EXISTS analysis_flows_analysis ON analysis_flows (analysis_id);

CREATE TABLE IF NOT EXISTS analysis_batches (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind text NOT NULL,
    state text NOT NULL,
    total integer NOT NULL DEFAULT 0,
    completed integer NOT NULL DEFAULT 0,
    failed integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL,
    finished_at timestamptz,
    CONSTRAINT analysis_batches_state CHECK (state IN ('running','completed','failed'))
);
CREATE INDEX IF NOT EXISTS analysis_batches_project ON analysis_batches (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS analysis_batches_created ON analysis_batches (created_at);

CREATE TABLE IF NOT EXISTS analysis_batch_calls (
    batch_id text NOT NULL REFERENCES analysis_batches(id) ON DELETE CASCADE,
    call_id text NOT NULL,
    failed boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (batch_id, call_id)
);

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

CREATE TABLE IF NOT EXISTS decisions (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    call_id text NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    policy_version bigint NOT NULL DEFAULT 0,
    phase text NOT NULL,
    action text NOT NULL,
    blocked boolean NOT NULL DEFAULT false,
    rule_id text NOT NULL DEFAULT '',
    code text NOT NULL DEFAULT '',
    severity text NOT NULL DEFAULT '',
    reason text NOT NULL DEFAULT '',
    attributes jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL,
    CONSTRAINT decisions_phase CHECK (phase IN ('request','tool_call','tool_result','response')),
    CONSTRAINT decisions_action CHECK (action IN ('block','record'))
);
CREATE INDEX IF NOT EXISTS decisions_call ON decisions (call_id, created_at);

CREATE TABLE IF NOT EXISTS analysis_jobs (
    id text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind text NOT NULL,
    scope jsonb NOT NULL DEFAULT '{}',
    state text NOT NULL DEFAULT 'pending',
    attempts integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 5,
    priority integer NOT NULL DEFAULT 0,
    run_after timestamptz NOT NULL DEFAULT now(),
    locked_by text NOT NULL DEFAULT '',
    lease_expires_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    dedupe_key text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT analysis_jobs_state CHECK (state IN ('pending','running','done','failed','cancelled'))
);
CREATE INDEX IF NOT EXISTS analysis_jobs_claim ON analysis_jobs (state, priority DESC, run_after) WHERE state IN ('pending','running');
CREATE INDEX IF NOT EXISTS analysis_jobs_project ON analysis_jobs (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS analysis_jobs_updated ON analysis_jobs (state, updated_at);

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
    redaction_hits bigint NOT NULL DEFAULT 0,
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
    schema_fingerprint text NOT NULL,
    hmac_secret bytea NOT NULL
);
