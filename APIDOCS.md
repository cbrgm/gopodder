# goPodder API v1

The goPodder API allows you to programmatically manage your podcast synchronization data. Use it to build automation scripts, custom integrations, or third-party tools that interact with your goPodder instance.

## Quick Start

```bash
# Create an API key in the web UI: Account > API Keys > Create Key
# Then use it to list your gPodder users:
curl -H "Authorization: Bearer gp_your_key_here" https://your-instance/api/v1/users
```

## Authentication

All API v1 endpoints require an API key passed as a Bearer token in the `Authorization` header:

```
Authorization: Bearer gp_<your-key>
```

API keys are created in the web UI under **Account > API Keys**. The key is shown once at creation and cannot be retrieved later.

### Key Roles

| Role | Access |
|------|--------|
| `standard` | Manage your own account's gPodder users, devices, and subscriptions |
| `admin` | Everything above, plus manage all accounts on the instance |

Admin keys can only be created by admin accounts. Standard keys are scoped to the account that created them.

### Limits

- Maximum 25 API keys per account
- Keys are identified by a `gp_` prefix
- Only the bcrypt hash is stored server-side

## Errors

All error responses return JSON with an `error` field:

```json
{"error": "description of what went wrong"}
```

| Status Code | Meaning |
|-------------|---------|
| 400 | Bad request (invalid input, validation failure) |
| 401 | Missing or invalid API key |
| 403 | Key does not have permission for this action |
| 404 | Resource not found |
| 409 | Conflict (resource already exists) |
| 500 | Internal server error |

## Endpoints

### Users

#### List Users

Returns all gPodder users belonging to your account.

```
GET /api/v1/users
```

**Required role:** `standard`

**Response:**

```json
[
  {
    "username": "alice",
    "last_activity": "2026-05-28T14:30:00"
  },
  {
    "username": "bob"
  }
]
```

The `last_activity` field is omitted when a user has never synced.

**Example:**

```bash
curl -H "Authorization: Bearer gp_your_key" https://your-instance/api/v1/users
```

---

#### Create User

Creates a new gPodder user linked to your account.

```
POST /api/v1/users
```

**Required role:** `standard`

**Request body:**

```json
{
  "username": "alice",
  "password": "a-strong-password"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `username` | string | yes | 1-64 characters, letters/numbers/dots/dashes/underscores |
| `password` | string | yes | Must meet the instance's minimum password length |

**Response (201):**

```json
{"username": "alice"}
```

**Errors:**

- `400` — Invalid username or password too short
- `403` — User creation is disabled, or user limit reached
- `409` — Username already exists

**Example:**

```bash
curl -X POST -H "Authorization: Bearer gp_your_key" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"my-secure-pass"}' \
  https://your-instance/api/v1/users
```

---

#### Delete User

Deletes a gPodder user and all associated data (devices, subscriptions, episode actions).

```
DELETE /api/v1/users/{username}
```

**Required role:** `standard`

**Response:** `204 No Content`

**Errors:**

- `403` — User does not belong to your account
- `404` — User not found

**Example:**

```bash
curl -X DELETE -H "Authorization: Bearer gp_your_key" \
  https://your-instance/api/v1/users/alice
```

---

### Devices

#### List Devices

Returns all devices registered for a user.

```
GET /api/v1/users/{username}/devices
```

**Required role:** `standard`

**Response:**

```json
[
  {
    "id": "pixel7",
    "caption": "My Pixel 7",
    "type": "mobile",
    "subscriptions": 42,
    "last_activity": "2026-05-28T14:30:00"
  }
]
```

The `last_activity` field shows when the device last synced. It is omitted if the device has never synced.

**Example:**

```bash
curl -H "Authorization: Bearer gp_your_key" \
  https://your-instance/api/v1/users/alice/devices
```

---

### Subscriptions

#### List Subscriptions (JSON)

Returns all active podcast feed URLs for a user.

```
GET /api/v1/users/{username}/subscriptions
```

**Required role:** `standard`

**Response:**

```json
[
  "https://feeds.example.com/podcast1.xml",
  "https://feeds.example.com/podcast2.xml"
]
```

**Example:**

```bash
curl -H "Authorization: Bearer gp_your_key" \
  https://your-instance/api/v1/users/alice/subscriptions
```

---

#### Export Subscriptions (OPML)

Returns subscriptions as an OPML document, suitable for importing into other podcast apps.

```
GET /api/v1/users/{username}/subscriptions.opml
```

**Required role:** `standard`

**Response:** `Content-Type: text/x-opml+xml`

**Example:**

```bash
curl -H "Authorization: Bearer gp_your_key" \
  https://your-instance/api/v1/users/alice/subscriptions.opml > subscriptions.opml
```

---

#### Update Subscriptions

Add or remove podcast feed subscriptions.

```
POST /api/v1/users/{username}/subscriptions
```

**Required role:** `standard`

**Request body:**

```json
{
  "add": ["https://feeds.example.com/new-podcast.xml"],
  "remove": ["https://feeds.example.com/old-podcast.xml"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `add` | string[] | no | Feed URLs to subscribe to |
| `remove` | string[] | no | Feed URLs to unsubscribe from |

Only `http://` and `https://` URLs are accepted. Invalid URLs are silently filtered. The same URL cannot appear in both `add` and `remove`.

**Response:**

```json
{"timestamp": 1716998400}
```

The `timestamp` value can be used with the gPodder sync protocol's `since` parameter to fetch changes made after this point.

**Example:**

```bash
curl -X POST -H "Authorization: Bearer gp_your_key" \
  -H "Content-Type: application/json" \
  -d '{"add":["https://feeds.example.com/new.xml"],"remove":[]}' \
  https://your-instance/api/v1/users/alice/subscriptions
```

---

### Accounts (Admin)

These endpoints require an API key with the `admin` role.

#### List Accounts

Returns all accounts on the instance.

```
GET /api/v1/accounts
```

**Required role:** `admin`

**Response:**

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "admin",
    "role": "admin",
    "created_at": "2026-01-15T10:00:00",
    "last_login": "2026-05-28T09:15:00",
    "last_activity": "2026-05-28T14:30:00"
  },
  {
    "id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
    "username": "team",
    "role": "standard",
    "created_at": "2026-03-01T12:00:00"
  }
]
```

The `last_login` field shows when the account last logged into the web UI. The `last_activity` field shows the most recent sync activity across all gPodder users linked to the account. Both are omitted if never occurred.

---

#### Create Account

Creates a new account (web UI login identity).

```
POST /api/v1/accounts
```

**Required role:** `admin`

**Request body:**

```json
{
  "username": "newteam",
  "password": "a-strong-password",
  "role": "standard"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `username` | string | yes | 1-64 characters |
| `password` | string | yes | Must meet minimum password length |
| `role` | string | no | `standard` (default) or `admin` |

**Response (201):**

```json
{"id": "550e8400-...", "role": "standard", "username": "newteam"}
```

---

#### Delete Account

Deletes a non-admin account and all its data (users, devices, subscriptions, episodes, API keys).

```
DELETE /api/v1/accounts/{id}
```

**Required role:** `admin`

**Response:** `204 No Content`

**Errors:**

- `400` — Cannot delete admin accounts via API (use the web UI)
- `404` — Account not found

---

#### List Account Users

Returns all gPodder users belonging to a specific account.

```
GET /api/v1/accounts/{id}/users
```

**Required role:** `admin`

**Response:**

```json
[
  {
    "username": "alice",
    "last_activity": "2026-05-28T14:30:00"
  }
]
```

---

## Usage Examples

### Backup all subscriptions to OPML files

```bash
#!/bin/bash
API_KEY="gp_your_key_here"
BASE_URL="https://your-instance"

for user in $(curl -s -H "Authorization: Bearer $API_KEY" "$BASE_URL/api/v1/users" | jq -r '.[].username'); do
  curl -s -H "Authorization: Bearer $API_KEY" \
    "$BASE_URL/api/v1/users/$user/subscriptions.opml" > "${user}.opml"
  echo "Exported $user"
done
```

### Sync subscriptions from an OPML file

```bash
#!/bin/bash
API_KEY="gp_your_key_here"
BASE_URL="https://your-instance"
USER="alice"

# Extract feed URLs from OPML
FEEDS=$(xmllint --xpath '//outline/@xmlUrl' subscriptions.opml 2>/dev/null | \
  grep -oP 'xmlUrl="\K[^"]+')

# Build JSON payload
ADD=$(echo "$FEEDS" | jq -R . | jq -s .)

curl -X POST -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"add\": $ADD, \"remove\": []}" \
  "$BASE_URL/api/v1/users/$USER/subscriptions"
```

### Provision a new user account

```bash
#!/bin/bash
API_KEY="gp_your_admin_key"
BASE_URL="https://your-instance"

# Create the web account
ACCT=$(curl -s -X POST -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"username":"newteam","password":"initial-pass-123","role":"standard"}' \
  "$BASE_URL/api/v1/accounts")

echo "Created account: $(echo $ACCT | jq -r .id)"
```

## Rate Limiting

There is currently no rate limiting on the API. Be considerate with request frequency, especially for endpoints that trigger bcrypt password hashing (authentication).

## Compatibility

The API v1 (`/api/v1/`) is separate from the gPodder sync protocol (`/api/2/`). The sync protocol is used by podcast apps (AntennaPod, gPodder, etc.) for subscription and episode synchronization. The API v1 is for administrative automation and tooling.
