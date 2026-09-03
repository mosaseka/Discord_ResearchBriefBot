CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    run_type TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    scope_days INTEGER,
    fetched_count INTEGER NOT NULL DEFAULT 0,
    selected_count INTEGER NOT NULL DEFAULT 0,
    posted_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'running',
    last_error TEXT
);

CREATE TABLE items (
    identity TEXT PRIMARY KEY,
    revision TEXT NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    doi TEXT,
    arxiv_id TEXT,
    pmid TEXT,
    source_name TEXT NOT NULL,
    source_type TEXT NOT NULL,
    published_at TEXT,
    updated_at TEXT,
    score INTEGER NOT NULL,
    selected INTEGER NOT NULL,
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
);

CREATE TABLE source_fetches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id),
    source_name TEXT NOT NULL,
    window_days INTEGER NOT NULL,
    checked_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('success','partial','failure')),
    item_count INTEGER NOT NULL,
    last_error TEXT
);

CREATE TABLE delivery_batches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id),
    channel_key TEXT NOT NULL,
    nonce TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('sending','sent','failed','unknown')),
    message_id TEXT,
    created_at TEXT NOT NULL,
    sent_at TEXT,
    last_error TEXT,
    UNIQUE(channel_key, nonce)
);

CREATE TABLE deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    batch_id INTEGER NOT NULL REFERENCES delivery_batches(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    channel_key TEXT NOT NULL,
    identity TEXT NOT NULL,
    revision TEXT NOT NULL,
    title TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('sending','sent','failed','unknown')),
    notified_at TEXT,
    UNIQUE(channel_key, identity, revision)
);

CREATE INDEX deliveries_lookup ON deliveries(channel_key, identity, revision, state);
