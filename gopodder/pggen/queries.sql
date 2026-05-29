-- Accounts
-- name: GetAccountByID :one
SELECT id, username, pwhash, role, session_id FROM accounts WHERE id = $1;

-- name: GetAccountByUsername :one
SELECT id, username, pwhash, role, session_id FROM accounts WHERE username = $1;

-- name: CreateAccount :exec
INSERT INTO accounts (id, username, pwhash, role, created_at) VALUES ($1, $2, $3, $4, $5);

-- name: UpdateAccountSession :exec
UPDATE accounts SET session_id = $1, session_created = $2 WHERE id = $3;

-- name: UpdateAccountLastLogin :exec
UPDATE accounts SET last_login = $1 WHERE id = $2;

-- name: GetAccountBySession :one
SELECT id, username, pwhash, role, session_id, session_created FROM accounts WHERE session_id = $1;

-- name: ListAccounts :many
SELECT a.id, a.username, a.role, a.created_at, a.last_login,
       (SELECT COUNT(*) FROM users u WHERE u.account_id = a.id) AS user_count,
       COALESCE((SELECT MAX(u.last_activity) FROM users u WHERE u.account_id = a.id), 0) AS last_activity
FROM accounts a
ORDER BY a.username;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = $1;

-- name: CountAccounts :one
SELECT COUNT(*) FROM accounts;

-- name: ListInactiveAccounts :many
SELECT a.id, a.username, a.role, a.created_at
FROM accounts a
WHERE a.role != 'admin'
  AND COALESCE(
    (SELECT MAX(u.last_activity) FROM users u WHERE u.account_id = a.id),
    a.created_at
  ) < $1;

-- name: UpdateAccountUsername :exec
UPDATE accounts SET username = $1 WHERE id = $2;

-- name: UpdateAccountPassword :exec
UPDATE accounts SET pwhash = $1 WHERE id = $2;

-- name: UpdateAccountRole :exec
UPDATE accounts SET role = $1 WHERE id = $2;

-- Users (gPodder API)
-- name: GetUser :one
SELECT username, pwhash, session_id, account_id, last_activity, share_token FROM users WHERE username = $1;

-- name: CreateUser :exec
INSERT INTO users (username, pwhash, account_id) VALUES ($1, $2, $3);

-- name: UpdateUserPassword :exec
UPDATE users SET pwhash = $1 WHERE username = $2;

-- name: UpdateUserSession :exec
UPDATE users SET session_id = $1, session_created = $2 WHERE username = $3;

-- name: UpdateUserLastActivity :exec
UPDATE users SET last_activity = $1 WHERE username = $2;

-- name: GetUserBySession :one
SELECT username, pwhash, session_id, session_created, account_id, last_activity FROM users WHERE session_id = $1;

-- name: UpdateUserShareToken :exec
UPDATE users SET share_token = $1 WHERE username = $2;

-- name: GetUserByShareToken :one
SELECT username, account_id, share_token FROM users WHERE share_token = $1;

-- name: ListUsers :many
SELECT u.username, u.account_id, a.username AS account_name
FROM users u
JOIN accounts a ON a.id = u.account_id
ORDER BY u.username;

-- name: ListUsersByAccount :many
SELECT username, account_id, last_activity FROM users WHERE account_id = $1 ORDER BY username;

-- name: ListUsersByAccountWithStats :many
SELECT u.username, u.account_id, u.last_activity,
       (SELECT COUNT(*) FROM devices d WHERE d.username = u.username) AS device_count,
       (SELECT COUNT(*) FROM subscriptions s WHERE s.username = u.username AND s.deleted IS NULL) AS subscription_count
FROM users u
WHERE u.account_id = $1
ORDER BY u.username;

-- name: DeleteUser :exec
DELETE FROM users WHERE username = $1;

-- name: DeleteUsersByAccount :exec
DELETE FROM users WHERE account_id = $1;

-- Devices
-- name: ListDevices :many
SELECT id, username, caption, type, last_activity FROM devices WHERE username = $1;

-- name: CreateDevice :exec
INSERT INTO devices (id, username, caption, type) VALUES ($1, $2, $3, $4)
ON CONFLICT (id, username) DO NOTHING;

-- name: UpdateDeviceCaption :exec
UPDATE devices SET caption = $1 WHERE id = $2 AND username = $3;

-- name: UpdateDeviceType :exec
UPDATE devices SET type = $1 WHERE id = $2 AND username = $3;

-- name: UpdateDeviceLastActivity :exec
UPDATE devices SET last_activity = $1 WHERE id = $2 AND username = $3;

-- name: DeleteDevice :exec
DELETE FROM devices WHERE id = $1 AND username = $2;

-- name: DeleteAllUserDevices :exec
DELETE FROM devices WHERE username = $1;

-- Subscriptions
-- name: GetSubscriptions :many
SELECT url FROM subscriptions WHERE username = $1 AND deleted IS NULL;

-- name: GetSubscriptionsSince :many
SELECT url, CASE WHEN deleted IS NULL THEN 0 ELSE 1 END AS is_deleted
FROM subscriptions
WHERE username = $1 AND (created >= $2 OR deleted >= $3);

-- name: AddSubscription :exec
INSERT INTO subscriptions (username, url, created)
VALUES ($1, $2, $3)
ON CONFLICT(url, username) DO UPDATE SET
    created = CASE WHEN subscriptions.deleted IS NOT NULL THEN EXCLUDED.created ELSE subscriptions.created END,
    deleted = NULL;

-- name: ReactivateSubscription :exec
UPDATE subscriptions SET created = $1, deleted = NULL
WHERE username = $2 AND url = $3 AND deleted IS NOT NULL;

-- name: DeleteSubscription :exec
UPDATE subscriptions SET deleted = $1
WHERE username = $2 AND url = $3 AND deleted IS NULL;

-- name: DeleteAllUserSubscriptions :exec
DELETE FROM subscriptions WHERE username = $1;

-- Episodes
-- name: GetEpisodes :many
SELECT username, device, podcast, episode, timestamp, guid, action,
       started, position, total, modified
FROM episodes
WHERE username = $1 AND modified >= $2;

-- name: GetEpisodesByPodcast :many
SELECT username, device, podcast, episode, timestamp, guid, action,
       started, position, total, modified
FROM episodes
WHERE username = $1 AND podcast = $2 AND modified >= $3;

-- name: GetEpisodesByDevice :many
SELECT username, device, podcast, episode, timestamp, guid, action,
       started, position, total, modified
FROM episodes
WHERE username = $1 AND device = $2 AND modified >= $3;

-- name: GetEpisodesByPodcastAndDevice :many
SELECT username, device, podcast, episode, timestamp, guid, action,
       started, position, total, modified
FROM episodes
WHERE username = $1 AND podcast = $2 AND device = $3 AND modified >= $4;

-- name: DeleteAllUserEpisodes :exec
DELETE FROM episodes WHERE username = $1;

-- name: DeleteEpisodesOlderThan :execresult
DELETE FROM episodes WHERE modified < $1;

-- name: UpsertEpisode :exec
INSERT INTO episodes (username, device, podcast, episode, timestamp, guid, action, started, position, total, modified, content_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT(username, podcast, episode) DO UPDATE SET
    device = EXCLUDED.device,
    timestamp = EXCLUDED.timestamp,
    guid = EXCLUDED.guid,
    action = EXCLUDED.action,
    started = EXCLUDED.started,
    position = EXCLUDED.position,
    total = EXCLUDED.total,
    modified = CASE WHEN EXCLUDED.content_hash != episodes.content_hash THEN EXCLUDED.modified ELSE episodes.modified END,
    content_hash = EXCLUDED.content_hash;

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
INSERT INTO api_keys (id, account_id, name, prefix, hash, role, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListAPIKeysByAccount :many
SELECT id, account_id, name, prefix, role, created_at, last_used FROM api_keys WHERE account_id = $1 ORDER BY created_at DESC;

-- name: GetAPIKeysByPrefix :many
SELECT id, account_id, name, prefix, hash, role, created_at, last_used FROM api_keys WHERE prefix = $1;

-- name: DeleteAPIKey :exec
DELETE FROM api_keys WHERE id = $1 AND account_id = $2;

-- name: DeleteAPIKeysByAccount :exec
DELETE FROM api_keys WHERE account_id = $1;

-- name: UpdateAPIKeyLastUsed :exec
UPDATE api_keys SET last_used = $1 WHERE id = $2;

-- name: CountAPIKeysByAccount :one
SELECT COUNT(*) FROM api_keys WHERE account_id = $1;

-- Settings
-- name: GetSetting :one
SELECT value FROM settings WHERE key = $1;

-- name: UpsertSetting :exec
INSERT INTO settings (key, value) VALUES ($1, $2)
ON CONFLICT(key) DO UPDATE SET value = EXCLUDED.value;
