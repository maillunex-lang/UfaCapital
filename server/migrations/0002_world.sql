-- World, ledger, production, market, contracts, agents, assets.

CREATE TABLE world_state (
    id int PRIMARY KEY CHECK (id = 1),
    tick bigint NOT NULL CHECK (tick >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE cities (
    code text PRIMARY KEY,
    title text NOT NULL,
    lat double precision NOT NULL,
    lon double precision NOT NULL
);

CREATE TABLE goods (
    code text PRIMARY KEY,
    title text NOT NULL,
    unit text NOT NULL,
    active boolean NOT NULL DEFAULT true
);

CREATE TABLE recipe_versions (
    id uuid PRIMARY KEY,
    code text NOT NULL,
    version int NOT NULL CHECK (version >= 1),
    inputs jsonb NOT NULL,
    outputs jsonb NOT NULL,
    energy_cost bigint NOT NULL CHECK (energy_cost >= 0),
    duration_ticks int NOT NULL CHECK (duration_ticks >= 1),
    effective_at timestamptz NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_by uuid REFERENCES users (id),
    UNIQUE (code, version)
);

CREATE TABLE config_versions (
    id uuid PRIMARY KEY,
    key text NOT NULL,
    version int NOT NULL CHECK (version >= 1),
    value jsonb NOT NULL,
    effective_at timestamptz NOT NULL,
    created_by uuid REFERENCES users (id),
    UNIQUE (key, version)
);

CREATE TABLE accounts (
    id uuid PRIMARY KEY,
    code text NOT NULL UNIQUE,
    owner_kind text NOT NULL,
    owner_id uuid,
    account_class text NOT NULL CHECK (account_class IN ('normal', 'emission', 'fund')),
    available bigint NOT NULL DEFAULT 0,
    reserved bigint NOT NULL DEFAULT 0,
    emission_limit bigint,
    CHECK (
        account_class = 'emission'
        OR (available >= 0 AND reserved >= 0)
    ),
    CHECK (account_class <> 'emission' OR reserved = 0)
);

CREATE TABLE ledger_transactions (
    id uuid PRIMARY KEY,
    operation_id uuid NOT NULL UNIQUE,
    kind text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ledger_entries (
    id bigserial PRIMARY KEY,
    tx_id uuid NOT NULL REFERENCES ledger_transactions (id),
    account_id uuid NOT NULL REFERENCES accounts (id),
    amount bigint NOT NULL CHECK (amount <> 0)
);

CREATE OR REPLACE FUNCTION uc_check_ledger_balanced() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    s bigint;
BEGIN
    SELECT COALESCE(SUM(amount), 0) INTO s FROM ledger_entries WHERE tx_id = NEW.tx_id;
    IF s <> 0 THEN
        RAISE EXCEPTION 'ledger imbalance for %', NEW.tx_id;
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER trg_ledger_balanced
AFTER INSERT ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION uc_check_ledger_balanced();

CREATE TABLE companies (
    id uuid PRIMARY KEY,
    owner_user_id uuid NOT NULL REFERENCES users (id),
    city_code text NOT NULL REFERENCES cities (code),
    name text NOT NULL,
    owner_kind text NOT NULL CHECK (owner_kind IN ('HUMAN', 'AI', 'TEST_ADMIN', 'SYSTEM')),
    status text NOT NULL CHECK (status IN ('active', 'restricted', 'closed')),
    cash_account_id uuid NOT NULL REFERENCES accounts (id),
    exclude_from_rankings boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX companies_one_active_per_user
    ON companies (owner_user_id)
    WHERE status <> 'closed';

CREATE TABLE enterprise_templates (
    code text PRIMARY KEY,
    title text NOT NULL,
    rent bigint NOT NULL CHECK (rent >= 0),
    expand_cost bigint NOT NULL CHECK (expand_cost >= 0),
    min_workers int NOT NULL CHECK (min_workers >= 0),
    storage bigint NOT NULL CHECK (storage > 0),
    book_value bigint NOT NULL CHECK (book_value >= 0),
    recipe_code text
);

CREATE TABLE enterprises (
    id uuid PRIMARY KEY,
    company_id uuid NOT NULL REFERENCES companies (id),
    template_code text NOT NULL REFERENCES enterprise_templates (code),
    city_code text NOT NULL REFERENCES cities (code),
    capacity int NOT NULL CHECK (capacity >= 1),
    status text NOT NULL CHECK (status IN ('running', 'stopped', 'closed')),
    pledged boolean NOT NULL DEFAULT false,
    created_tick bigint NOT NULL
);

CREATE TABLE workforce (
    enterprise_id uuid PRIMARY KEY REFERENCES enterprises (id),
    headcount int NOT NULL CHECK (headcount >= 0),
    wage bigint NOT NULL CHECK (wage >= 0)
);

CREATE TABLE warehouses (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL UNIQUE REFERENCES enterprises (id),
    capacity bigint NOT NULL CHECK (capacity > 0)
);

CREATE TABLE inventory_lots (
    id uuid PRIMARY KEY,
    warehouse_id uuid NOT NULL REFERENCES warehouses (id),
    company_id uuid NOT NULL REFERENCES companies (id),
    good_code text NOT NULL REFERENCES goods (code),
    qty bigint NOT NULL CHECK (qty >= 0),
    reserved bigint NOT NULL DEFAULT 0 CHECK (reserved >= 0 AND reserved <= qty),
    unit_cost bigint NOT NULL CHECK (unit_cost >= 0),
    created_tick bigint NOT NULL,
    expires_tick bigint,
    quality text NOT NULL DEFAULT 'base'
);

CREATE TABLE inventory_moves (
    id uuid PRIMARY KEY,
    operation_id uuid NOT NULL,
    lot_id uuid,
    good_code text NOT NULL,
    qty bigint NOT NULL,
    kind text NOT NULL,
    company_id uuid,
    created_tick bigint NOT NULL
);

CREATE TABLE money_reservations (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts (id),
    amount bigint NOT NULL CHECK (amount > 0),
    reason text NOT NULL,
    ref_id uuid NOT NULL,
    active boolean NOT NULL DEFAULT true
);

CREATE TABLE lot_reservations (
    id uuid PRIMARY KEY,
    lot_id uuid NOT NULL REFERENCES inventory_lots (id),
    qty bigint NOT NULL CHECK (qty > 0),
    ref_id uuid NOT NULL,
    active boolean NOT NULL DEFAULT true
);

CREATE TABLE production_jobs (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises (id),
    recipe_version_id uuid NOT NULL REFERENCES recipe_versions (id),
    start_tick bigint NOT NULL,
    complete_tick bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'done', 'cancelled')),
    input_qty jsonb NOT NULL,
    output_qty jsonb NOT NULL,
    wage_reserved bigint NOT NULL CHECK (wage_reserved >= 0),
    energy_reserved bigint NOT NULL CHECK (energy_reserved >= 0),
    UNIQUE (enterprise_id, start_tick)
);

CREATE TABLE market_orders (
    id uuid PRIMARY KEY,
    company_id uuid NOT NULL REFERENCES companies (id),
    city_code text NOT NULL REFERENCES cities (code),
    good_code text NOT NULL REFERENCES goods (code),
    side text NOT NULL CHECK (side IN ('buy', 'sell')),
    price bigint NOT NULL CHECK (price > 0),
    qty_total bigint NOT NULL CHECK (qty_total > 0),
    qty_open bigint NOT NULL CHECK (qty_open >= 0),
    status text NOT NULL CHECK (status IN ('open', 'filled', 'cancelled')),
    created_tick bigint NOT NULL,
    seq bigserial
);

CREATE INDEX market_orders_book_idx
    ON market_orders (city_code, good_code, side, status, price, created_tick, seq);

CREATE TABLE trades (
    id uuid PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES market_orders (id),
    buyer_company_id uuid NOT NULL REFERENCES companies (id),
    seller_company_id uuid NOT NULL REFERENCES companies (id),
    good_code text NOT NULL,
    qty bigint NOT NULL CHECK (qty > 0),
    price bigint NOT NULL CHECK (price > 0),
    total bigint NOT NULL CHECK (total > 0),
    tick bigint NOT NULL,
    operation_id uuid NOT NULL UNIQUE
);

CREATE TABLE contracts (
    id uuid PRIMARY KEY,
    seller_company_id uuid NOT NULL REFERENCES companies (id),
    buyer_company_id uuid REFERENCES companies (id),
    city_code text NOT NULL,
    good_code text NOT NULL,
    qty bigint NOT NULL CHECK (qty > 0),
    price bigint NOT NULL CHECK (price > 0),
    deadline_tick bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('offered', 'accepted', 'delivered', 'cancelled', 'expired')),
    created_tick bigint NOT NULL
);

CREATE TABLE deliveries (
    id uuid PRIMARY KEY,
    contract_id uuid NOT NULL UNIQUE REFERENCES contracts (id),
    arrive_tick bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('transit', 'done'))
);

CREATE TABLE loans (
    id uuid PRIMARY KEY,
    company_id uuid NOT NULL REFERENCES companies (id),
    principal bigint NOT NULL CHECK (principal > 0),
    outstanding bigint NOT NULL CHECK (outstanding >= 0),
    rate_bps int NOT NULL CHECK (rate_bps >= 0),
    collateral_enterprise_id uuid NOT NULL REFERENCES enterprises (id),
    next_interest_tick bigint NOT NULL,
    due_tick bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'overdue', 'repaid', 'defaulted')),
    overdue_since_tick bigint
);

CREATE TABLE population_state (
    city_code text PRIMARY KEY REFERENCES cities (code),
    account_id uuid NOT NULL REFERENCES accounts (id),
    budget_remaining bigint NOT NULL CHECK (budget_remaining >= 0)
);

CREATE TABLE external_limits (
    city_code text NOT NULL,
    good_code text NOT NULL,
    day_index bigint NOT NULL,
    supplied bigint NOT NULL CHECK (supplied >= 0),
    PRIMARY KEY (city_code, good_code, day_index)
);

CREATE TABLE agents (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL UNIQUE REFERENCES users (id),
    company_id uuid NOT NULL UNIQUE REFERENCES companies (id),
    profile text NOT NULL,
    specialization text NOT NULL,
    decision_seed bigint NOT NULL,
    paused boolean NOT NULL DEFAULT false,
    strategy text NOT NULL,
    cooldown_until_tick bigint NOT NULL DEFAULT 0,
    loss_streak int NOT NULL DEFAULT 0
);

CREATE TABLE agent_memory (
    agent_id uuid NOT NULL REFERENCES agents (id),
    key text NOT NULL,
    value jsonb NOT NULL,
    updated_tick bigint NOT NULL,
    PRIMARY KEY (agent_id, key)
);

CREATE TABLE decision_log (
    id uuid PRIMARY KEY,
    agent_id uuid NOT NULL REFERENCES agents (id),
    tick bigint NOT NULL,
    candidates jsonb NOT NULL,
    selected text NOT NULL,
    reason text NOT NULL,
    result jsonb NOT NULL DEFAULT '{}'
);

CREATE TABLE vip_entitlements (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id),
    starts_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    source text NOT NULL,
    created_by uuid REFERENCES users (id),
    revoked_at timestamptz,
    CHECK (expires_at > starts_at)
);

CREATE TABLE assets (
    asset_id text PRIMARY KEY,
    semantic_type text NOT NULL,
    usage text NOT NULL,
    requires_apk boolean NOT NULL DEFAULT false,
    current_version_id uuid
);

CREATE TABLE asset_versions (
    id uuid PRIMARY KEY,
    asset_id text NOT NULL REFERENCES assets (asset_id),
    version int NOT NULL CHECK (version >= 1),
    prompt text NOT NULL CHECK (char_length(prompt) > 0),
    provenance text NOT NULL,
    license text NOT NULL,
    attribution text NOT NULL DEFAULT '',
    mime text NOT NULL,
    width int NOT NULL CHECK (width > 0),
    height int NOT NULL CHECK (height > 0),
    byte_size int NOT NULL CHECK (byte_size > 0),
    sha256 text NOT NULL,
    storage_path text NOT NULL,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (asset_id, version)
);

CREATE TABLE asset_manifests (
    id uuid PRIMARY KEY,
    version int NOT NULL UNIQUE,
    document jsonb NOT NULL,
    active boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE world_ticks (
    tick bigint PRIMARY KEY,
    operation_id uuid NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
