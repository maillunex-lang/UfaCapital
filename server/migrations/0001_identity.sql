-- Identity, sessions, audit, idempotency, outbox. No world economy yet.
-- schema_migrations is created by the migrator before these files run.

CREATE TABLE users (
    id uuid PRIMARY KEY,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    display_name text NOT NULL,
    is_ai boolean NOT NULL DEFAULT false,
    grant_consumed boolean NOT NULL DEFAULT false,
    blocked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (email = lower(email)),
    CHECK (char_length(display_name) BETWEEN 1 AND 80)
);

CREATE TABLE user_roles (
    user_id uuid NOT NULL REFERENCES users (id),
    role text NOT NULL,
    PRIMARY KEY (user_id, role),
    CHECK (role IN (
        'player', 'admin_read', 'catalog_editor', 'economy_admin',
        'moderator', 'asset_editor', 'test_operator', 'superadmin'
    ))
);

CREATE TABLE user_totp (
    user_id uuid PRIMARY KEY REFERENCES users (id),
    secret bytea NOT NULL,
    enabled boolean NOT NULL DEFAULT false
);

CREATE TABLE mfa_challenges (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id),
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id),
    family_id uuid NOT NULL,
    channel text NOT NULL CHECK (channel IN ('api', 'admin')),
    access_hash bytea NOT NULL UNIQUE,
    access_expires_at timestamptz NOT NULL,
    csrf_secret text,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    hash bytea PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES sessions (id),
    family_id uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    revoked_at timestamptz
);

CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);

CREATE TABLE login_attempts (
    id bigserial PRIMARY KEY,
    email text NOT NULL,
    ip text NOT NULL DEFAULT '',
    success boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX login_attempts_email_idx ON login_attempts (email, created_at DESC);

CREATE TABLE audit_log (
    id uuid PRIMARY KEY,
    actor_id uuid REFERENCES users (id),
    action text NOT NULL,
    reason text NOT NULL DEFAULT '',
    before_state jsonb NOT NULL DEFAULT '{}',
    after_state jsonb NOT NULL DEFAULT '{}',
    correlation_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_keys (
    actor_id uuid NOT NULL,
    route text NOT NULL,
    idem_key text NOT NULL,
    body_hash bytea NOT NULL,
    status_code int NOT NULL,
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, route, idem_key)
);

CREATE TABLE outbox (
    id bigserial PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE,
    topic text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
