package gopodder

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/cbrgm/gopodder/gopodder/pggen"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed pggen/schema.sql
var pgSchemaFS embed.FS

type PostgresStore struct {
	db      *sql.DB
	queries *pggen.Queries
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := migratePostgres(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &PostgresStore{
		db:      db,
		queries: pggen.New(db),
	}, nil
}

func migratePostgres(db *sql.DB) error {
	schema, err := pgSchemaFS.ReadFile("pggen/schema.sql")
	if err != nil {
		return err
	}
	_, err = db.Exec(string(schema))
	return err
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// Accounts

func (s *PostgresStore) GetAccount(ctx context.Context, username string) (*Account, error) {
	row, err := s.queries.GetAccountByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	return &Account{
		ID:        row.ID,
		Username:  row.Username,
		PWHash:    row.Pwhash,
		Role:      row.Role,
		SessionID: nullStringPtr(row.SessionID),
	}, nil
}

func (s *PostgresStore) GetAccountByID(ctx context.Context, id string) (*Account, error) {
	row, err := s.queries.GetAccountByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Account{
		ID:        row.ID,
		Username:  row.Username,
		PWHash:    row.Pwhash,
		Role:      row.Role,
		SessionID: nullStringPtr(row.SessionID),
	}, nil
}

func (s *PostgresStore) CreateAccount(ctx context.Context, id, username, pwhash, role string, createdAt time.Time) error {
	return s.queries.CreateAccount(ctx, pggen.CreateAccountParams{
		ID:        id,
		Username:  username,
		Pwhash:    pwhash,
		Role:      role,
		CreatedAt: createdAt.Unix(),
	})
}

func (s *PostgresStore) UpdateAccountSession(ctx context.Context, id string, sessionID *string, now time.Time) error {
	var sessionCreated sql.NullInt64
	if sessionID != nil {
		sessionCreated = sql.NullInt64{Int64: now.Unix(), Valid: true}
	}
	return s.queries.UpdateAccountSession(ctx, pggen.UpdateAccountSessionParams{
		SessionID:      ptrToNullString(sessionID),
		SessionCreated: sessionCreated,
		ID:             id,
	})
}

func (s *PostgresStore) UpdateAccountLastLogin(ctx context.Context, id string, t time.Time) error {
	return s.queries.UpdateAccountLastLogin(ctx, pggen.UpdateAccountLastLoginParams{
		LastLogin: sql.NullInt64{Int64: t.Unix(), Valid: true},
		ID:        id,
	})
}

func (s *PostgresStore) GetAccountBySession(ctx context.Context, sessionID string) (*Account, error) {
	row, err := s.queries.GetAccountBySession(ctx, sql.NullString{String: sessionID, Valid: true})
	if err != nil {
		return nil, err
	}
	return &Account{
		ID:             row.ID,
		Username:       row.Username,
		PWHash:         row.Pwhash,
		Role:           row.Role,
		SessionID:      nullStringPtr(row.SessionID),
		SessionCreated: nullInt64ToTime(row.SessionCreated),
	}, nil
}

func (s *PostgresStore) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.queries.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(rows))
	for _, row := range rows {
		acct := Account{
			ID:        row.ID,
			Username:  row.Username,
			Role:      row.Role,
			CreatedAt: time.Unix(row.CreatedAt, 0),
			UserCount: row.UserCount,
		}
		if row.LastLogin.Valid {
			acct.LastLogin = new(time.Unix(row.LastLogin.Int64, 0))
		}
		if v := toInt64(row.LastActivity); v != 0 {
			acct.LastActivity = new(time.Unix(v, 0))
		}
		accounts = append(accounts, acct)
	}
	return accounts, nil
}

func (s *PostgresStore) DeleteAccount(ctx context.Context, id string) error {
	return s.queries.DeleteAccount(ctx, id)
}

func (s *PostgresStore) ListInactiveAccounts(ctx context.Context, cutoff int64) ([]Account, error) {
	rows, err := s.queries.ListInactiveAccounts(ctx, sql.NullInt64{Int64: cutoff, Valid: true})
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, Account{
			ID:        row.ID,
			Username:  row.Username,
			Role:      row.Role,
			CreatedAt: time.Unix(row.CreatedAt, 0),
		})
	}
	return accounts, nil
}

func (s *PostgresStore) CountAccounts(ctx context.Context) (int64, error) {
	return s.queries.CountAccounts(ctx)
}

func (s *PostgresStore) UpdateAccountUsername(ctx context.Context, id, username string) error {
	return s.queries.UpdateAccountUsername(ctx, pggen.UpdateAccountUsernameParams{
		Username: username,
		ID:       id,
	})
}

func (s *PostgresStore) UpdateAccountPassword(ctx context.Context, id, pwhash string) error {
	return s.queries.UpdateAccountPassword(ctx, pggen.UpdateAccountPasswordParams{
		Pwhash: pwhash,
		ID:     id,
	})
}

func (s *PostgresStore) UpdateAccountRole(ctx context.Context, id, role string) error {
	return s.queries.UpdateAccountRole(ctx, pggen.UpdateAccountRoleParams{
		Role: role,
		ID:   id,
	})
}

// Users

func (s *PostgresStore) GetUser(ctx context.Context, username string) (*User, error) {
	row, err := s.queries.GetUser(ctx, username)
	if err != nil {
		return nil, err
	}
	return &User{
		Username:     row.Username,
		PWHash:       row.Pwhash,
		SessionID:    nullStringPtr(row.SessionID),
		AccountID:    row.AccountID,
		LastActivity: nullInt64ToTime(row.LastActivity),
		ShareToken:   nullStringPtr(row.ShareToken),
	}, nil
}

func (s *PostgresStore) CreateUser(ctx context.Context, username, pwhash, accountID string) error {
	return s.queries.CreateUser(ctx, pggen.CreateUserParams{
		Username:  username,
		Pwhash:    pwhash,
		AccountID: accountID,
	})
}

func (s *PostgresStore) UpdateUserPassword(ctx context.Context, username, pwhash string) error {
	return s.queries.UpdateUserPassword(ctx, pggen.UpdateUserPasswordParams{
		Pwhash:   pwhash,
		Username: username,
	})
}

func (s *PostgresStore) UpdateUserLastActivity(ctx context.Context, username string, t time.Time) error {
	return s.queries.UpdateUserLastActivity(ctx, pggen.UpdateUserLastActivityParams{
		LastActivity: sql.NullInt64{Int64: t.Unix(), Valid: true},
		Username:     username,
	})
}

func (s *PostgresStore) UpdateUserSession(ctx context.Context, username string, sessionID *string, now time.Time) error {
	var sessionCreated sql.NullInt64
	if sessionID != nil {
		sessionCreated = sql.NullInt64{Int64: now.Unix(), Valid: true}
	}
	return s.queries.UpdateUserSession(ctx, pggen.UpdateUserSessionParams{
		SessionID:      ptrToNullString(sessionID),
		SessionCreated: sessionCreated,
		Username:       username,
	})
}

func (s *PostgresStore) GetUserBySession(ctx context.Context, sessionID string) (*User, error) {
	row, err := s.queries.GetUserBySession(ctx, sql.NullString{String: sessionID, Valid: true})
	if err != nil {
		return nil, err
	}
	return &User{
		Username:       row.Username,
		PWHash:         row.Pwhash,
		SessionID:      nullStringPtr(row.SessionID),
		SessionCreated: nullInt64ToTime(row.SessionCreated),
		AccountID:      row.AccountID,
		LastActivity:   nullInt64ToTime(row.LastActivity),
	}, nil
}

func (s *PostgresStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.queries.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(rows))
	for _, row := range rows {
		users = append(users, User{
			Username:  row.Username,
			AccountID: row.AccountID,
		})
	}
	return users, nil
}

func (s *PostgresStore) ListUsersByAccount(ctx context.Context, accountID string) ([]User, error) {
	rows, err := s.queries.ListUsersByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(rows))
	for _, row := range rows {
		u := User{
			Username:  row.Username,
			AccountID: row.AccountID,
		}
		if row.LastActivity.Valid {
			u.LastActivity = new(time.Unix(row.LastActivity.Int64, 0))
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *PostgresStore) ListUsersByAccountWithStats(ctx context.Context, accountID string) ([]UserWithStats, error) {
	rows, err := s.queries.ListUsersByAccountWithStats(ctx, accountID)
	if err != nil {
		return nil, err
	}
	users := make([]UserWithStats, 0, len(rows))
	for _, row := range rows {
		users = append(users, UserWithStats{
			Username:      row.Username,
			AccountID:     row.AccountID,
			LastActivity:  nullInt64ToTime(row.LastActivity),
			Devices:       row.DeviceCount,
			Subscriptions: row.SubscriptionCount,
		})
	}
	return users, nil
}

func (s *PostgresStore) DeleteUser(ctx context.Context, username string) error {
	return s.queries.DeleteUser(ctx, username)
}

func (s *PostgresStore) DeleteUsersByAccount(ctx context.Context, accountID string) error {
	return s.queries.DeleteUsersByAccount(ctx, accountID)
}

func (s *PostgresStore) SetUserShareToken(ctx context.Context, username string, token *string) error {
	return s.queries.UpdateUserShareToken(ctx, pggen.UpdateUserShareTokenParams{
		ShareToken: ptrToNullString(token),
		Username:   username,
	})
}

func (s *PostgresStore) GetUserByShareToken(ctx context.Context, token string) (*User, error) {
	row, err := s.queries.GetUserByShareToken(ctx, sql.NullString{String: token, Valid: true})
	if err != nil {
		return nil, err
	}
	return &User{
		Username:   row.Username,
		AccountID:  row.AccountID,
		ShareToken: nullStringPtr(row.ShareToken),
	}, nil
}

// Devices

func (s *PostgresStore) ListDevices(ctx context.Context, username string) ([]Device, error) {
	rows, err := s.queries.ListDevices(ctx, username)
	if err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(rows))
	for _, row := range rows {
		d := Device{
			ID:      row.ID,
			Caption: nullStringVal(row.Caption),
			Type:    row.Type,
		}
		if row.LastActivity.Valid {
			d.LastActivity = new(time.Unix(row.LastActivity.Int64, 0))
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func (s *PostgresStore) UpsertDevice(ctx context.Context, username, deviceID string, dev DeviceUpdate) error {
	if err := s.queries.CreateDevice(ctx, pggen.CreateDeviceParams{
		ID:       deviceID,
		Username: username,
		Caption:  ptrToNullString(dev.Caption),
		Type:     ptrStringOr(dev.Type, "other"),
	}); err != nil {
		return err
	}

	if dev.Caption != nil {
		if err := s.queries.UpdateDeviceCaption(ctx, pggen.UpdateDeviceCaptionParams{
			Caption:  ptrToNullString(dev.Caption),
			ID:       deviceID,
			Username: username,
		}); err != nil {
			return err
		}
	}
	if dev.Type != nil {
		if err := s.queries.UpdateDeviceType(ctx, pggen.UpdateDeviceTypeParams{
			Type:     *dev.Type,
			ID:       deviceID,
			Username: username,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) UpdateDeviceLastActivity(ctx context.Context, username, deviceID string, t time.Time) error {
	return s.queries.UpdateDeviceLastActivity(ctx, pggen.UpdateDeviceLastActivityParams{
		LastActivity: sql.NullInt64{Int64: t.Unix(), Valid: true},
		ID:           deviceID,
		Username:     username,
	})
}

func (s *PostgresStore) DeleteDevice(ctx context.Context, username, deviceID string) error {
	return s.queries.DeleteDevice(ctx, pggen.DeleteDeviceParams{
		ID:       deviceID,
		Username: username,
	})
}

func (s *PostgresStore) DeleteAllUserDevices(ctx context.Context, username string) error {
	return s.queries.DeleteAllUserDevices(ctx, username)
}

// Subscriptions

func (s *PostgresStore) GetSubscriptions(ctx context.Context, username string) ([]string, error) {
	return s.queries.GetSubscriptions(ctx, username)
}

func (s *PostgresStore) GetSubscriptionChanges(ctx context.Context, username string, since int64) (*SubscriptionChanges, error) {
	rows, err := s.queries.GetSubscriptionsSince(ctx, pggen.GetSubscriptionsSinceParams{
		Username: username,
		Created:  since,
		Deleted:  sql.NullInt64{Int64: since, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	changes := &SubscriptionChanges{}
	for _, row := range rows {
		if row.IsDeleted == 1 {
			changes.Remove = append(changes.Remove, row.Url)
		} else {
			changes.Add = append(changes.Add, row.Url)
		}
	}
	return changes, nil
}

func (s *PostgresStore) UpdateSubscriptions(ctx context.Context, username string, add, remove []string, timestamp int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.applySubscriptionChanges(ctx, s.queries.WithTx(tx), username, add, remove, timestamp); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) ReplaceSubscriptions(ctx context.Context, username string, desired []string, timestamp int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	q := s.queries.WithTx(tx)

	existing, err := q.GetSubscriptions(ctx, username)
	if err != nil {
		return err
	}

	add, remove := diffSubscriptions(existing, desired)
	for _, url := range add {
		_ = q.ReactivateSubscription(ctx, pggen.ReactivateSubscriptionParams{
			Created: timestamp, Username: username, Url: url,
		})
	}
	if err := s.applySubscriptionChanges(ctx, q, username, add, remove, timestamp); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) applySubscriptionChanges(ctx context.Context, q *pggen.Queries, username string, add, remove []string, timestamp int64) error {
	for _, url := range add {
		if err := q.AddSubscription(ctx, pggen.AddSubscriptionParams{
			Username: username,
			Url:      url,
			Created:  timestamp,
		}); err != nil {
			return err
		}
	}
	for _, url := range remove {
		if err := q.DeleteSubscription(ctx, pggen.DeleteSubscriptionParams{
			Deleted:  sql.NullInt64{Int64: timestamp, Valid: true},
			Username: username,
			Url:      url,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) ReactivateSubscription(ctx context.Context, username, url string, timestamp int64) error {
	return s.queries.ReactivateSubscription(ctx, pggen.ReactivateSubscriptionParams{
		Created:  timestamp,
		Username: username,
		Url:      url,
	})
}

func (s *PostgresStore) DeleteAllUserSubscriptions(ctx context.Context, username string) error {
	return s.queries.DeleteAllUserSubscriptions(ctx, username)
}

// Episodes

func (s *PostgresStore) GetEpisodes(ctx context.Context, params EpisodeQuery) ([]Episode, error) {
	switch {
	case params.Podcast != nil && params.Device != nil:
		rows, err := s.queries.GetEpisodesByPodcastAndDevice(ctx, pggen.GetEpisodesByPodcastAndDeviceParams{
			Username: params.Username,
			Podcast:  *params.Podcast,
			Device:   sql.NullString{String: *params.Device, Valid: true},
			Modified: params.Since,
		})
		if err != nil {
			return nil, err
		}
		eps := make([]Episode, 0, len(rows))
		for _, r := range rows {
			eps = append(eps, toEpisode(r.Device, r.Podcast, r.Episode, r.Timestamp, r.Guid, r.Action, r.Started, r.Position, r.Total))
		}
		return eps, nil

	case params.Podcast != nil:
		rows, err := s.queries.GetEpisodesByPodcast(ctx, pggen.GetEpisodesByPodcastParams{
			Username: params.Username,
			Podcast:  *params.Podcast,
			Modified: params.Since,
		})
		if err != nil {
			return nil, err
		}
		eps := make([]Episode, 0, len(rows))
		for _, r := range rows {
			eps = append(eps, toEpisode(r.Device, r.Podcast, r.Episode, r.Timestamp, r.Guid, r.Action, r.Started, r.Position, r.Total))
		}
		return eps, nil

	case params.Device != nil:
		rows, err := s.queries.GetEpisodesByDevice(ctx, pggen.GetEpisodesByDeviceParams{
			Username: params.Username,
			Device:   sql.NullString{String: *params.Device, Valid: true},
			Modified: params.Since,
		})
		if err != nil {
			return nil, err
		}
		eps := make([]Episode, 0, len(rows))
		for _, r := range rows {
			eps = append(eps, toEpisode(r.Device, r.Podcast, r.Episode, r.Timestamp, r.Guid, r.Action, r.Started, r.Position, r.Total))
		}
		return eps, nil

	default:
		rows, err := s.queries.GetEpisodes(ctx, pggen.GetEpisodesParams{
			Username: params.Username,
			Modified: params.Since,
		})
		if err != nil {
			return nil, err
		}
		eps := make([]Episode, 0, len(rows))
		for _, r := range rows {
			eps = append(eps, toEpisode(r.Device, r.Podcast, r.Episode, r.Timestamp, r.Guid, r.Action, r.Started, r.Position, r.Total))
		}
		return eps, nil
	}
}

func (s *PostgresStore) UpdateEpisodes(ctx context.Context, username string, episodes []Episode, timestamp int64) error {
	for i := 0; i < len(episodes); i += episodeBatchSize {
		end := i + episodeBatchSize
		if end > len(episodes) {
			end = len(episodes)
		}
		if err := s.upsertEpisodeBatch(ctx, username, episodes[i:end], timestamp); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) upsertEpisodeBatch(ctx context.Context, username string, episodes []Episode, timestamp int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	q := s.queries.WithTx(tx)

	for _, ep := range episodes {
		if err := q.UpsertEpisode(ctx, pggen.UpsertEpisodeParams{
			Username:    username,
			Device:      ptrToNullString(ep.Device),
			Podcast:     ep.Podcast,
			Episode:     ep.Episode,
			Timestamp:   isoTimestampToNullInt64(ep.Timestamp),
			Guid:        ptrToNullString(ep.GUID),
			Action:      ep.Action,
			Started:     ptrToNullInt64(ep.Started),
			Position:    ptrToNullInt64(ep.Position),
			Total:       ptrToNullInt64(ep.Total),
			Modified:    timestamp,
			ContentHash: episodeHash(ep),
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *PostgresStore) DeleteAllUserEpisodes(ctx context.Context, username string) error {
	return s.queries.DeleteAllUserEpisodes(ctx, username)
}

func (s *PostgresStore) DeleteEpisodesOlderThan(ctx context.Context, cutoff int64) (int64, error) {
	result, err := s.queries.DeleteEpisodesOlderThan(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// API Keys

func (s *PostgresStore) CreateAPIKey(ctx context.Context, key APIKey) error {
	return s.queries.CreateAPIKey(ctx, pggen.CreateAPIKeyParams{
		ID:        key.ID,
		AccountID: key.AccountID,
		Name:      key.Name,
		Prefix:    key.Prefix,
		Hash:      key.Hash,
		Role:      key.Role,
		CreatedAt: key.CreatedAt.Unix(),
	})
}

func (s *PostgresStore) ListAPIKeysByAccount(ctx context.Context, accountID string) ([]APIKey, error) {
	rows, err := s.queries.ListAPIKeysByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	keys := make([]APIKey, 0, len(rows))
	for _, row := range rows {
		k := APIKey{
			ID:        row.ID,
			AccountID: row.AccountID,
			Name:      row.Name,
			Prefix:    row.Prefix,
			Role:      row.Role,
			CreatedAt: time.Unix(row.CreatedAt, 0),
			LastUsed:  nullInt64ToTime(row.LastUsed),
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *PostgresStore) GetAPIKeysByPrefix(ctx context.Context, prefix string) ([]APIKey, error) {
	rows, err := s.queries.GetAPIKeysByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}
	keys := make([]APIKey, 0, len(rows))
	for _, row := range rows {
		k := APIKey{
			ID:        row.ID,
			AccountID: row.AccountID,
			Name:      row.Name,
			Prefix:    row.Prefix,
			Hash:      row.Hash,
			Role:      row.Role,
			CreatedAt: time.Unix(row.CreatedAt, 0),
			LastUsed:  nullInt64ToTime(row.LastUsed),
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *PostgresStore) DeleteAPIKey(ctx context.Context, id, accountID string) error {
	return s.queries.DeleteAPIKey(ctx, pggen.DeleteAPIKeyParams{
		ID:        id,
		AccountID: accountID,
	})
}

func (s *PostgresStore) DeleteAPIKeysByAccount(ctx context.Context, accountID string) error {
	return s.queries.DeleteAPIKeysByAccount(ctx, accountID)
}

func (s *PostgresStore) UpdateAPIKeyLastUsed(ctx context.Context, id string, t time.Time) error {
	return s.queries.UpdateAPIKeyLastUsed(ctx, pggen.UpdateAPIKeyLastUsedParams{
		LastUsed: sql.NullInt64{Int64: t.Unix(), Valid: true},
		ID:       id,
	})
}

func (s *PostgresStore) CountAPIKeysByAccount(ctx context.Context, accountID string) (int64, error) {
	return s.queries.CountAPIKeysByAccount(ctx, accountID)
}

// Stats

func (s *PostgresStore) GetStats(ctx context.Context) (Stats, error) {
	accounts, err := s.queries.CountAccounts(ctx)
	if err != nil {
		return Stats{}, err
	}
	users, err := s.queries.CountUsers(ctx)
	if err != nil {
		return Stats{}, err
	}
	devices, err := s.queries.CountDevices(ctx)
	if err != nil {
		return Stats{}, err
	}
	subs, err := s.queries.CountActiveSubscriptions(ctx)
	if err != nil {
		return Stats{}, err
	}
	episodes, err := s.queries.CountEpisodes(ctx)
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		Accounts:      accounts,
		Users:         users,
		Devices:       devices,
		Subscriptions: subs,
		Episodes:      episodes,
	}, nil
}

// Settings

func (s *PostgresStore) GetSetting(ctx context.Context, key string) (string, error) {
	return s.queries.GetSetting(ctx, key)
}

func (s *PostgresStore) SetSetting(ctx context.Context, key, value string) error {
	return s.queries.UpsertSetting(ctx, pggen.UpsertSettingParams{
		Key:   key,
		Value: value,
	})
}
