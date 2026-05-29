CREATE TABLE IF NOT EXISTS accounts (
    id TEXT NOT NULL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    pwhash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'standard',
    session_id TEXT,
    session_created INTEGER,
    created_at INTEGER NOT NULL DEFAULT 0,
    last_login INTEGER
);

CREATE TABLE IF NOT EXISTS users (
    username TEXT NOT NULL PRIMARY KEY,
    pwhash TEXT NOT NULL,
    session_id TEXT,
    session_created INTEGER,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    last_activity INTEGER,
    share_token TEXT
);

CREATE TABLE IF NOT EXISTS devices (
    id TEXT NOT NULL,
    username TEXT NOT NULL,
    caption TEXT,
    type TEXT NOT NULL DEFAULT 'other',
    last_activity INTEGER,
    UNIQUE(id, username)
);

CREATE TABLE IF NOT EXISTS subscriptions (
    username TEXT NOT NULL,
    url TEXT NOT NULL,
    created INTEGER NOT NULL,
    deleted INTEGER,
    UNIQUE(url, username)
);

CREATE TABLE IF NOT EXISTS episodes (
    username TEXT NOT NULL,
    device TEXT,
    podcast TEXT NOT NULL,
    episode TEXT NOT NULL,
    timestamp INTEGER,
    guid TEXT,
    action TEXT NOT NULL,
    started INTEGER,
    position INTEGER,
    total INTEGER,
    modified INTEGER NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    UNIQUE(username, podcast, episode)
);

CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT NOT NULL PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'standard',
    created_at INTEGER NOT NULL,
    last_used INTEGER
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_accounts_session_id ON accounts(session_id) WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_session_id ON users(session_id) WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_account_id ON users(account_id);
CREATE INDEX IF NOT EXISTS idx_devices_username ON devices(username);
CREATE INDEX IF NOT EXISTS idx_subscriptions_username ON subscriptions(username) WHERE deleted IS NULL;
CREATE INDEX IF NOT EXISTS idx_subscriptions_username_created ON subscriptions(username, created);
CREATE INDEX IF NOT EXISTS idx_subscriptions_username_deleted ON subscriptions(username, deleted) WHERE deleted IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_episodes_username_modified ON episodes(username, modified);
CREATE INDEX IF NOT EXISTS idx_users_share_token ON users(share_token) WHERE share_token IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_account_id ON api_keys(account_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
