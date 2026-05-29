-- Accounts
-- name: GetAccountByID :one
SELECT id, username, pwhash, role, session_id FROM accounts WHERE id = ?;

-- name: GetAccountByUsername :one
SELECT id, username, pwhash, role, session_id FROM accounts WHERE username = ?;

-- name: CreateAccount :exec
INSERT INTO accounts (id, username, pwhash, role, created_at) VALUES (?, ?, ?, ?, ?);

-- name: UpdateAccountSession :exec
UPDATE accounts SET session_id = ?, session_created = ? WHERE id = ?;

-- name: UpdateAccountLastLogin :exec
UPDATE accounts SET last_login = ? WHERE id = ?;

-- name: GetAccountBySession :one
SELECT id, username, pwhash, role, session_id, session_created FROM accounts WHERE session_id = ?;

-- name: ListAccounts :many
SELECT a.id, a.username, a.role, a.created_at, a.last_login,
       (SELECT COUNT(*) FROM users u WHERE u.account_id = a.id) AS user_count,
       COALESCE((SELECT MAX(u.last_activity) FROM users u WHERE u.account_id = a.id), 0) AS last_activity
FROM accounts a
ORDER BY a.username;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ?;

-- name: CountAccounts :one
SELECT COUNT(*) FROM accounts;

-- name: ListInactiveAccounts :many
SELECT a.id, a.username, a.role, a.created_at
FROM accounts a
WHERE a.role != 'admin'
  AND COALESCE(
    (SELECT MAX(u.last_activity) FROM users u WHERE u.account_id = a.id),
    a.created_at
  ) < ?;

-- name: UpdateAccountUsername :exec
UPDATE accounts SET username = ? WHERE id = ?;

-- name: UpdateAccountPassword :exec
UPDATE accounts SET pwhash = ? WHERE id = ?;

-- name: UpdateAccountRole :exec
UPDATE accounts SET role = ? WHERE id = ?;

-- Users (gPodder API)
-- name: GetUser :one
SELECT username, pwhash, session_id, account_id, last_activity, share_token FROM users WHERE username = ?;

-- name: CreateUser :exec
INSERT INTO users (username, pwhash, account_id) VALUES (?, ?, ?);

-- name: UpdateUserPassword :exec
UPDATE users SET pwhash = ? WHERE username = ?;

-- name: UpdateUserSession :exec
UPDATE users SET session_id = ?, session_created = ? WHERE username = ?;

-- name: UpdateUserLastActivity :exec
UPDATE users SET last_activity = ? WHERE username = ?;

-- name: GetUserBySession :one
SELECT username, pwhash, session_id, session_created, account_id, last_activity FROM users WHERE session_id = ?;

-- name: UpdateUserShareToken :exec
UPDATE users SET share_token = ? WHERE username = ?;

-- name: GetUserByShareToken :one
SELECT username, account_id, share_token FROM users WHERE share_token = ?;

-- name: ListUsers :many
SELECT u.username, u.account_id, a.username AS account_name
FROM users u
JOIN accounts a ON a.id = u.account_id
ORDER BY u.username;

-- name: ListUsersByAccount :many
SELECT username, account_id, last_activity FROM users WHERE account_id = ? ORDER BY username;

-- name: ListUsersByAccountWithStats :many
SELECT u.username, u.account_id, u.last_activity,
       (SELECT COUNT(*) FROM devices d WHERE d.username = u.username) AS device_count,
       (SELECT COUNT(*) FROM subscriptions s WHERE s.username = u.username AND s.deleted IS NULL) AS subscription_count
FROM users u
WHERE u.account_id = ?
ORDER BY u.username;

-- name: DeleteUser :exec
DELETE FROM users WHERE username = ?;

-- name: DeleteUsersByAccount :exec
DELETE FROM users WHERE account_id = ?;

-- Devices
-- name: ListDevices :many
SELECT id, username, caption, type, last_activity FROM devices WHERE username = ?;

-- name: CreateDevice :exec
INSERT OR IGNORE INTO devices (id, username, caption, type) VALUES (?, ?, ?, ?);

-- name: UpdateDeviceCaption :exec
UPDATE devices SET caption = ? WHERE id = ? AND username = ?;

-- name: UpdateDeviceType :exec
UPDATE devices SET type = ? WHERE id = ? AND username = ?;

-- name: UpdateDeviceLastActivity :exec
UPDATE devices SET last_activity = ? WHERE id = ? AND username = ?;

-- name: DeleteDevice :exec
DELETE FROM devices WHERE id = ? AND username = ?;

-- name: DeleteAllUserDevices :exec
DELETE FROM devices WHERE username = ?;

-- Subscriptions
-- name: GetSubscriptions :many
SELECT url FROM subscriptions WHERE username = ? AND deleted IS NULL;

-- name: GetSubscriptionsSince :many
SELECT url, CASE WHEN deleted IS NULL THEN 0 ELSE 1 END AS is_deleted
FROM subscriptions
WHERE username = ? AND (created >= ? OR deleted >= ?);

-- name: AddSubscription :exec
INSERT INTO subscriptions (username, url, created)
VALUES (?, ?, ?)
ON CONFLICT(url, username) DO UPDATE SET
    created = CASE WHEN subscriptions.deleted IS NOT NULL THEN excluded.created ELSE subscriptions.created END,
    deleted = NULL;

-- name: ReactivateSubscription :exec
UPDATE subscriptions SET created = ?, deleted = NULL
WHERE username = ? AND url = ? AND deleted IS NOT NULL;

-- name: DeleteSubscription :exec
UPDATE subscriptions SET deleted = ?
WHERE username = ? AND url = ? AND deleted IS NULL;

-- name: DeleteAllUserSubscriptions :exec
DELETE FROM subscriptions WHERE username = ?;

-- Episodes
-- name: GetEpisodes :many
SELECT username, device, podcast, episode, timestamp, guid, action,
       started, position, total, modified
FROM episodes
WHERE username = ? AND modified >= ?;

-- name: GetEpisodesByPodcast :many
SELECT username, device, podcast, episode, timestamp, guid, action,
       started, position, total, modified
FROM episodes
WHERE username = ? AND podcast = ? AND modified >= ?;

-- name: GetEpisodesByDevice :many
SELECT username, device, podcast, episode, timestamp, guid, action,
       started, position, total, modified
FROM episodes
WHERE username = ? AND device = ? AND modified >= ?;

-- name: GetEpisodesByPodcastAndDevice :many
SELECT username, device, podcast, episode, timestamp, guid, action,
       started, position, total, modified
FROM episodes
WHERE username = ? AND podcast = ? AND device = ? AND modified >= ?;

-- name: DeleteAllUserEpisodes :exec
DELETE FROM episodes WHERE username = ?;

-- name: DeleteEpisodesOlderThan :execresult
DELETE FROM episodes WHERE modified < ?;

-- name: UpsertEpisode :exec
INSERT INTO episodes (username, device, podcast, episode, timestamp, guid, action, started, position, total, modified, content_hash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(username, podcast, episode) DO UPDATE SET
    device = excluded.device,
    timestamp = excluded.timestamp,
    guid = excluded.guid,
    action = excluded.action,
    started = excluded.started,
    position = excluded.position,
    total = excluded.total,
    modified = CASE WHEN excluded.content_hash != episodes.content_hash THEN excluded.modified ELSE episodes.modified END,
    content_hash = excluded.content_hash;

-- Stats
-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CountDevices :one
SELECT COUNT(*) FROM devices;

-- name: CountActiveSubscriptions :one
SELECT COUNT(*) FROM subscriptions WHERE deleted IS NULL;

-- name: CountEpisodes :one
SELECT COUNT(*) FROM episodes;

-- API Keys
-- name: CreateAPIKey :exec
INSERT INTO api_keys (id, account_id, name, prefix, hash, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListAPIKeysByAccount :many
SELECT id, account_id, name, prefix, role, created_at, last_used FROM api_keys WHERE account_id = ? ORDER BY created_at DESC;

-- name: GetAPIKeysByPrefix :many
SELECT id, account_id, name, prefix, hash, role, created_at, last_used FROM api_keys WHERE prefix = ?;

-- name: DeleteAPIKey :exec
DELETE FROM api_keys WHERE id = ? AND account_id = ?;

-- name: DeleteAPIKeysByAccount :exec
DELETE FROM api_keys WHERE account_id = ?;

-- name: UpdateAPIKeyLastUsed :exec
UPDATE api_keys SET last_used = ? WHERE id = ?;

-- name: CountAPIKeysByAccount :one
SELECT COUNT(*) FROM api_keys WHERE account_id = ?;

-- Settings
-- name: GetSetting :one
SELECT value FROM settings WHERE key = ?;

-- name: UpsertSetting :exec
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
