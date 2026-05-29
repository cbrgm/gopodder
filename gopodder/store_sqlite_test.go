package gopodder

import (
	"database/sql"
	"testing"
	"time"
)

const testAccountID = "test-account-id"

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateAccount(t.Context(), testAccountID, "testaccount", "hash", RoleAdmin, time.Now()); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return store
}

func TestSQLiteStore_CreateAndGetUser(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	pwhash := hashPassword("secret")
	if err := store.CreateUser(ctx, "alice", pwhash, testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user, err := store.GetUser(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Username != "alice" {
		t.Errorf("username = %q, want %q", user.Username, "alice")
	}
	if user.PWHash != pwhash {
		t.Errorf("pwhash mismatch")
	}
	if user.SessionID != nil {
		t.Errorf("session should be nil initially, got %q", *user.SessionID)
	}
}

func TestSQLiteStore_GetUser_NotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.GetUser(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestSQLiteStore_CreateUser_Duplicate(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash1", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	err := store.CreateUser(ctx, "alice", "hash2", testAccountID)
	if err == nil {
		t.Fatal("expected error for duplicate user")
	}
}

func TestSQLiteStore_SessionManagement(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "bob", hashPassword("pass"), testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sid := "session-123"
	if err := store.UpdateUserSession(ctx, "bob", &sid, time.Now()); err != nil {
		t.Fatalf("UpdateUserSession: %v", err)
	}

	user, err := store.GetUserBySession(ctx, "session-123")
	if err != nil {
		t.Fatalf("GetUserBySession: %v", err)
	}
	if user.Username != "bob" {
		t.Errorf("username = %q, want %q", user.Username, "bob")
	}

	if err := store.UpdateUserSession(ctx, "bob", nil, time.Time{}); err != nil {
		t.Fatalf("UpdateUserSession(nil): %v", err)
	}

	_, err = store.GetUserBySession(ctx, "session-123")
	if err == nil {
		t.Fatal("expected error after session cleared")
	}
}

func TestSQLiteStore_GetUserBySession_NotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.GetUserBySession(ctx, "nonexistent-session")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestSQLiteStore_Devices(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	t.Run("list empty", func(t *testing.T) {
		devices, err := store.ListDevices(ctx, "alice")
		if err != nil {
			t.Fatalf("ListDevices: %v", err)
		}
		if len(devices) != 0 {
			t.Errorf("expected 0 devices, got %d", len(devices))
		}
	})

	t.Run("upsert creates device", func(t *testing.T) {
		caption := "My Phone"
		typ := "mobile"
		if err := store.UpsertDevice(ctx, "alice", "phone1", DeviceUpdate{Caption: &caption, Type: &typ}); err != nil {
			t.Fatalf("UpsertDevice: %v", err)
		}

		devices, err := store.ListDevices(ctx, "alice")
		if err != nil {
			t.Fatalf("ListDevices: %v", err)
		}
		if len(devices) != 1 {
			t.Fatalf("expected 1 device, got %d", len(devices))
		}
		if devices[0].ID != "phone1" {
			t.Errorf("id = %q, want %q", devices[0].ID, "phone1")
		}
		if devices[0].Caption != "My Phone" {
			t.Errorf("caption = %q, want %q", devices[0].Caption, "My Phone")
		}
		if devices[0].Type != "mobile" {
			t.Errorf("type = %q, want %q", devices[0].Type, "mobile")
		}
	})

	t.Run("upsert updates device", func(t *testing.T) {
		newCaption := "Updated Phone"
		if err := store.UpsertDevice(ctx, "alice", "phone1", DeviceUpdate{Caption: &newCaption}); err != nil {
			t.Fatalf("UpsertDevice: %v", err)
		}

		devices, err := store.ListDevices(ctx, "alice")
		if err != nil {
			t.Fatalf("ListDevices: %v", err)
		}
		if devices[0].Caption != "Updated Phone" {
			t.Errorf("caption = %q, want %q", devices[0].Caption, "Updated Phone")
		}
		if devices[0].Type != "mobile" {
			t.Errorf("type should remain %q, got %q", "mobile", devices[0].Type)
		}
	})

	t.Run("upsert with nil fields uses defaults", func(t *testing.T) {
		if err := store.UpsertDevice(ctx, "alice", "laptop1", DeviceUpdate{}); err != nil {
			t.Fatalf("UpsertDevice: %v", err)
		}

		devices, err := store.ListDevices(ctx, "alice")
		if err != nil {
			t.Fatalf("ListDevices: %v", err)
		}
		var found bool
		for _, d := range devices {
			if d.ID == "laptop1" {
				found = true
				if d.Type != "other" {
					t.Errorf("type = %q, want %q", d.Type, "other")
				}
			}
		}
		if !found {
			t.Error("laptop1 not found")
		}
	})
}

func TestSQLiteStore_Subscriptions(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	t.Run("get empty subscriptions", func(t *testing.T) {
		subs, err := store.GetSubscriptions(ctx, "alice")
		if err != nil {
			t.Fatalf("GetSubscriptions: %v", err)
		}
		if len(subs) != 0 {
			t.Errorf("expected 0 subscriptions, got %d", len(subs))
		}
	})

	t.Run("add subscriptions", func(t *testing.T) {
		err := store.UpdateSubscriptions(ctx, "alice",
			[]string{"http://feed1.com/rss", "http://feed2.com/rss"}, nil, 100)
		if err != nil {
			t.Fatalf("UpdateSubscriptions: %v", err)
		}

		subs, err := store.GetSubscriptions(ctx, "alice")
		if err != nil {
			t.Fatalf("GetSubscriptions: %v", err)
		}
		if len(subs) != 2 {
			t.Fatalf("expected 2 subscriptions, got %d", len(subs))
		}
	})

	t.Run("remove subscription", func(t *testing.T) {
		err := store.UpdateSubscriptions(ctx, "alice",
			nil, []string{"http://feed1.com/rss"}, 200)
		if err != nil {
			t.Fatalf("UpdateSubscriptions: %v", err)
		}

		subs, err := store.GetSubscriptions(ctx, "alice")
		if err != nil {
			t.Fatalf("GetSubscriptions: %v", err)
		}
		if len(subs) != 1 {
			t.Fatalf("expected 1 subscription, got %d", len(subs))
		}
		if subs[0] != "http://feed2.com/rss" {
			t.Errorf("remaining sub = %q, want %q", subs[0], "http://feed2.com/rss")
		}
	})

	t.Run("subscription changes since timestamp", func(t *testing.T) {
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 0)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Add) != 1 {
			t.Errorf("expected 1 add, got %d: %v", len(changes.Add), changes.Add)
		}
		if len(changes.Remove) != 1 {
			t.Errorf("expected 1 remove, got %d: %v", len(changes.Remove), changes.Remove)
		}
	})

	t.Run("subscription changes since future timestamp", func(t *testing.T) {
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 9999)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Add) != 0 {
			t.Errorf("expected 0 adds, got %d", len(changes.Add))
		}
		if len(changes.Remove) != 0 {
			t.Errorf("expected 0 removes, got %d", len(changes.Remove))
		}
	})

	t.Run("re-adding active subscription does not create duplicate", func(t *testing.T) {
		err := store.UpdateSubscriptions(ctx, "alice",
			[]string{"http://feed2.com/rss"}, nil, 300)
		if err != nil {
			t.Fatalf("UpdateSubscriptions: %v", err)
		}

		subs, err := store.GetSubscriptions(ctx, "alice")
		if err != nil {
			t.Fatalf("GetSubscriptions: %v", err)
		}
		count := 0
		for _, s := range subs {
			if s == "http://feed2.com/rss" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected exactly 1 instance of feed2, got %d (total subs: %v)", count, subs)
		}
	})

	t.Run("re-adding deleted subscription reactivates it", func(t *testing.T) {
		err := store.UpdateSubscriptions(ctx, "alice",
			[]string{"http://feed1.com/rss"}, nil, 400)
		if err != nil {
			t.Fatalf("UpdateSubscriptions: %v", err)
		}

		subs, err := store.GetSubscriptions(ctx, "alice")
		if err != nil {
			t.Fatalf("GetSubscriptions: %v", err)
		}
		var found bool
		for _, s := range subs {
			if s == "http://feed1.com/rss" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected feed1 to be reactivated, got %v", subs)
		}
	})

	t.Run("re-adding active subscription does not bump created timestamp", func(t *testing.T) {
		// feed2 is active since t=100. Re-add at t=500 should NOT move created.
		err := store.UpdateSubscriptions(ctx, "alice",
			[]string{"http://feed2.com/rss"}, nil, 500)
		if err != nil {
			t.Fatalf("UpdateSubscriptions: %v", err)
		}

		// Polling since=400 should NOT show feed2 as a change
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 400)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		for _, url := range changes.Add {
			if url == "http://feed2.com/rss" {
				t.Error("re-adding active subscription should not appear as new change in poll")
			}
		}
	})
}

func TestSQLiteStore_ReplaceSubscriptionsRemovesMissing(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com", "http://c.com"}, nil, 100); err != nil {
		t.Fatalf("add subs: %v", err)
	}

	// PUT with only 2 of the 3 — the missing one should be soft-deleted
	if err := store.ReplaceSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com"}, 200); err != nil {
		t.Fatalf("ReplaceSubscriptions: %v", err)
	}

	subs, _ := store.GetSubscriptions(ctx, "alice")
	if len(subs) != 2 {
		t.Errorf("expected 2 subs after replace (c.com removed), got %d: %v", len(subs), subs)
	}

	// The removed one should show up in changes feed
	changes, _ := store.GetSubscriptionChanges(ctx, "alice", 150)
	var foundRemove bool
	for _, u := range changes.Remove {
		if u == "http://c.com" {
			foundRemove = true
		}
	}
	if !foundRemove {
		t.Errorf("expected http://c.com in remove list after replace, got %v", changes.Remove)
	}
}

func TestSQLiteStore_ReplaceSubscriptionsAddsNew(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com"}, nil, 100); err != nil {
		t.Fatalf("add sub: %v", err)
	}

	if err := store.ReplaceSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com", "http://c.com"}, 200); err != nil {
		t.Fatalf("ReplaceSubscriptions: %v", err)
	}

	subs, _ := store.GetSubscriptions(ctx, "alice")
	if len(subs) != 3 {
		t.Errorf("expected 3 subs after additive replace, got %d: %v", len(subs), subs)
	}
}

func TestSQLiteStore_DeletedSubNotReechoedOnNextSync(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com"}, nil, 100); err != nil {
		t.Fatalf("add subs: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://a.com"}, 200); err != nil {
		t.Fatalf("remove sub: %v", err)
	}

	// Simulate: client receives changes with since=100, server returns timestamp=201 (now+1)
	// Client uses since=201 on next sync
	changes, err := store.GetSubscriptionChanges(ctx, "alice", 201)
	if err != nil {
		t.Fatalf("GetSubscriptionChanges: %v", err)
	}
	if len(changes.Add) != 0 {
		t.Errorf("expected 0 adds with since=201, got %v", changes.Add)
	}
	if len(changes.Remove) != 0 {
		t.Errorf("expected 0 removes with since=201 (deletion at t=200 should not re-echo), got %v", changes.Remove)
	}
}

func TestSQLiteStore_WebUIDeleteThenAppSync(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Add subscriptions (simulating initial sync from app)
	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com", "http://c.com"}, nil, 100); err != nil {
		t.Fatalf("add subs: %v", err)
	}

	// Web UI deletes one subscription at t=200
	if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://b.com"}, 200); err != nil {
		t.Fatalf("web UI delete: %v", err)
	}

	// App syncs with since=101 (its last sync was right after initial add)
	changes, err := store.GetSubscriptionChanges(ctx, "alice", 101)
	if err != nil {
		t.Fatalf("GetSubscriptionChanges: %v", err)
	}

	// Should see http://b.com in remove
	var foundRemove bool
	for _, u := range changes.Remove {
		if u == "http://b.com" {
			foundRemove = true
		}
	}
	if !foundRemove {
		t.Errorf("expected http://b.com in remove list, got add=%v remove=%v", changes.Add, changes.Remove)
	}

	// Should NOT see http://b.com in add
	for _, u := range changes.Add {
		if u == "http://b.com" {
			t.Error("deleted subscription should not appear in add list")
		}
	}
}

func TestSQLiteStore_AppDeleteThenWebUIShows(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com"}, nil, 100); err != nil {
		t.Fatalf("add subs: %v", err)
	}

	// App deletes one
	if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://a.com"}, 200); err != nil {
		t.Fatalf("app delete: %v", err)
	}

	// Web UI loads (GetSubscriptions returns only active)
	subs, _ := store.GetSubscriptions(ctx, "alice")
	if len(subs) != 1 {
		t.Errorf("expected 1 active sub after app delete, got %d: %v", len(subs), subs)
	}
	if len(subs) > 0 && subs[0] != "http://b.com" {
		t.Errorf("expected http://b.com to remain, got %v", subs)
	}
}

func TestSQLiteStore_SubscriptionIsolation(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.CreateUser(ctx, "bob", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://alice.com"}, nil, 100); err != nil {
		t.Fatalf("UpdateSubscriptions alice: %v", err)
	}
	if err := store.UpdateSubscriptions(ctx, "bob", []string{"http://bob.com"}, nil, 100); err != nil {
		t.Fatalf("UpdateSubscriptions bob: %v", err)
	}

	subs, _ := store.GetSubscriptions(ctx, "alice")
	if len(subs) != 1 || subs[0] != "http://alice.com" {
		t.Errorf("alice subs = %v, want [http://alice.com]", subs)
	}

	subs, _ = store.GetSubscriptions(ctx, "bob")
	if len(subs) != 1 || subs[0] != "http://bob.com" {
		t.Errorf("bob subs = %v, want [http://bob.com]", subs)
	}
}

func TestSQLiteStore_SubscriptionChanges_IncludesDeletions(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com"}, nil, 100); err != nil {
		t.Fatalf("UpdateSubscriptions add: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://a.com"}, 300); err != nil {
		t.Fatalf("UpdateSubscriptions remove: %v", err)
	}

	t.Run("since before both operations returns all changes", func(t *testing.T) {
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 0)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Add) != 1 || changes.Add[0] != "http://b.com" {
			t.Errorf("add = %v, want [http://b.com]", changes.Add)
		}
		if len(changes.Remove) != 1 || changes.Remove[0] != "http://a.com" {
			t.Errorf("remove = %v, want [http://a.com]", changes.Remove)
		}
	})

	t.Run("since between add and delete only returns deletion", func(t *testing.T) {
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 200)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Add) != 0 {
			t.Errorf("add = %v, want []", changes.Add)
		}
		if len(changes.Remove) != 1 || changes.Remove[0] != "http://a.com" {
			t.Errorf("remove = %v, want [http://a.com]", changes.Remove)
		}
	})

	t.Run("since after all operations returns nothing", func(t *testing.T) {
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 9999)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Add) != 0 {
			t.Errorf("add = %v, want []", changes.Add)
		}
		if len(changes.Remove) != 0 {
			t.Errorf("remove = %v, want []", changes.Remove)
		}
	})
}

func TestSQLiteStore_SoftDeletePropagatesToChangesFeed(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	t.Run("soft-delete on single device shows in changes when no other device has it", func(t *testing.T) {
		if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://only-phone.com"}, nil, 100); err != nil {
			t.Fatalf("add sub: %v", err)
		}
		if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://only-phone.com"}, 200); err != nil {
			t.Fatalf("remove sub: %v", err)
		}

		changes, err := store.GetSubscriptionChanges(ctx, "alice", 150)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Remove) != 1 || changes.Remove[0] != "http://only-phone.com" {
			t.Errorf("remove = %v, want [http://only-phone.com]", changes.Remove)
		}
	})

	t.Run("soft-delete from all devices shows as removed", func(t *testing.T) {
		if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://shared.com"}, nil, 300); err != nil {
			t.Fatalf("add phone: %v", err)
		}
		if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://shared.com"}, nil, 300); err != nil {
			t.Fatalf("add web: %v", err)
		}

		if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://shared.com"}, 400); err != nil {
			t.Fatalf("remove phone: %v", err)
		}
		if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://shared.com"}, 400); err != nil {
			t.Fatalf("remove web: %v", err)
		}

		changes, err := store.GetSubscriptionChanges(ctx, "alice", 350)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		found := false
		for _, u := range changes.Remove {
			if u == "http://shared.com" {
				found = true
			}
		}
		if !found {
			t.Errorf("http://shared.com not in remove list: %v", changes.Remove)
		}
	})

	t.Run("active subscriptions list is empty after soft-delete", func(t *testing.T) {
		if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://temp.com"}, nil, 500); err != nil {
			t.Fatalf("add: %v", err)
		}
		if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://temp.com"}, 600); err != nil {
			t.Fatalf("remove: %v", err)
		}

		active, _ := store.GetSubscriptions(ctx, "alice")
		if len(active) != 0 {
			t.Errorf("expected 0 active subs, got %d: %v", len(active), active)
		}
	})
}

func TestSQLiteStore_Episodes(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	device := "phone1"
	ts := "2024-06-01T14:30:00"
	var started, position, total int64 = 0, 120, 3600

	episodes := []Episode{
		{
			Podcast:   "http://podcast1.com/feed",
			Episode:   "http://podcast1.com/ep1.mp3",
			Device:    &device,
			Timestamp: &ts,
			Action:    "play",
			Started:   &started,
			Position:  &position,
			Total:     &total,
		},
		{
			Podcast: "http://podcast2.com/feed",
			Episode: "http://podcast2.com/ep1.mp3",
			Action:  "download",
		},
	}

	t.Run("upload episodes", func(t *testing.T) {
		if err := store.UpdateEpisodes(ctx, "alice", episodes, 100); err != nil {
			t.Fatalf("UpdateEpisodes: %v", err)
		}
	})

	t.Run("get all episodes", func(t *testing.T) {
		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Since: 0})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		if len(eps) != 2 {
			t.Fatalf("expected 2 episodes, got %d", len(eps))
		}
	})

	t.Run("get episodes with timestamp filter", func(t *testing.T) {
		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Since: 9999})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		if len(eps) != 0 {
			t.Errorf("expected 0 episodes with future since, got %d", len(eps))
		}
	})

	t.Run("get episodes filtered by podcast", func(t *testing.T) {
		podcast := "http://podcast1.com/feed"
		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Podcast: &podcast, Since: 0})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		if len(eps) != 1 {
			t.Fatalf("expected 1 episode, got %d", len(eps))
		}
		if eps[0].Podcast != podcast {
			t.Errorf("podcast = %q, want %q", eps[0].Podcast, podcast)
		}
	})

	t.Run("get episodes filtered by device", func(t *testing.T) {
		d := "phone1"
		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Device: &d, Since: 0})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		if len(eps) != 1 {
			t.Fatalf("expected 1 episode with device filter, got %d", len(eps))
		}
	})

	t.Run("get episodes filtered by podcast and device", func(t *testing.T) {
		podcast := "http://podcast1.com/feed"
		d := "phone1"
		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Podcast: &podcast, Device: &d, Since: 0})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		if len(eps) != 1 {
			t.Fatalf("expected 1 episode, got %d", len(eps))
		}
	})

	t.Run("episode timestamp is ISO 8601", func(t *testing.T) {
		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Since: 0})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		for _, ep := range eps {
			if ep.Timestamp != nil && *ep.Timestamp != "2024-06-01T14:30:00" {
				t.Errorf("timestamp = %q, want ISO 8601 format", *ep.Timestamp)
			}
		}
	})

	t.Run("episode fields preserved", func(t *testing.T) {
		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Since: 0})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		var playEp *Episode
		for i := range eps {
			if eps[i].Action == "play" {
				playEp = &eps[i]
				break
			}
		}
		if playEp == nil {
			t.Fatal("play episode not found")
		}
		if playEp.Started == nil || *playEp.Started != 0 {
			t.Errorf("started = %v, want 0", playEp.Started)
		}
		if playEp.Position == nil || *playEp.Position != 120 {
			t.Errorf("position = %v, want 120", playEp.Position)
		}
		if playEp.Total == nil || *playEp.Total != 3600 {
			t.Errorf("total = %v, want 3600", playEp.Total)
		}
		if playEp.Device == nil || *playEp.Device != "phone1" {
			t.Errorf("device = %v, want phone1", playEp.Device)
		}
	})

	t.Run("upsert updates existing episode", func(t *testing.T) {
		newPosition := int64(240)
		updated := []Episode{{
			Podcast:  "http://podcast1.com/feed",
			Episode:  "http://podcast1.com/ep1.mp3",
			Device:   &device,
			Action:   "play",
			Started:  &started,
			Position: &newPosition,
			Total:    &total,
		}}
		if err := store.UpdateEpisodes(ctx, "alice", updated, 200); err != nil {
			t.Fatalf("UpdateEpisodes: %v", err)
		}

		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Since: 0})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		if len(eps) != 2 {
			t.Fatalf("expected 2 episodes (upsert should not create duplicates), got %d", len(eps))
		}

		for _, ep := range eps {
			if ep.Episode == "http://podcast1.com/ep1.mp3" {
				if ep.Position == nil || *ep.Position != 240 {
					t.Errorf("position = %v, want 240", ep.Position)
				}
			}
		}
	})

	t.Run("content hash prevents modified timestamp update on identical data", func(t *testing.T) {
		same := []Episode{{
			Podcast:  "http://podcast1.com/feed",
			Episode:  "http://podcast1.com/ep1.mp3",
			Device:   &device,
			Action:   "play",
			Started:  &started,
			Position: func() *int64 { v := int64(240); return &v }(),
			Total:    &total,
		}}
		if err := store.UpdateEpisodes(ctx, "alice", same, 999); err != nil {
			t.Fatalf("UpdateEpisodes: %v", err)
		}

		eps, err := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Since: 300})
		if err != nil {
			t.Fatalf("GetEpisodes: %v", err)
		}
		for _, ep := range eps {
			if ep.Episode == "http://podcast1.com/ep1.mp3" {
				t.Error("episode should not appear with since=300 because content hash is unchanged")
			}
		}
	})
}

func TestSQLiteStore_CrossDeviceSubscriptionSync(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com/feed", "http://b.com/feed"}, nil, 100); err != nil {
		t.Fatalf("UpdateSubscriptions phone: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://c.com/feed"}, nil, 200); err != nil {
		t.Fatalf("UpdateSubscriptions laptop: %v", err)
	}

	t.Run("additions from one source visible in changes", func(t *testing.T) {
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 150)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Add) != 1 || changes.Add[0] != "http://c.com/feed" {
			t.Errorf("add = %v, want [http://c.com/feed]", changes.Add)
		}
	})

	t.Run("additions visible in changes for any query", func(t *testing.T) {
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 0)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Add) < 3 {
			t.Errorf("add = %v, want all 3 feeds", changes.Add)
		}
	})

	t.Run("removal on phone visible to laptop", func(t *testing.T) {
		if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://a.com/feed"}, 300); err != nil {
			t.Fatalf("UpdateSubscriptions remove: %v", err)
		}
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 250)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		if len(changes.Remove) != 1 || changes.Remove[0] != "http://a.com/feed" {
			t.Errorf("remove = %v, want [http://a.com/feed]", changes.Remove)
		}
	})

	t.Run("GetSubscriptions shows active subs", func(t *testing.T) {
		subs, err := store.GetSubscriptions(ctx, "alice")
		if err != nil {
			t.Fatalf("GetAllSubscriptions: %v", err)
		}
		if len(subs) != 2 {
			t.Errorf("expected 2 active subscriptions (b.com + c.com), got %d: %v", len(subs), subs)
		}
	})
}

func TestSQLiteStore_UserScopedRemoval(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://shared.com/feed"}, nil, 50); err != nil {
		t.Fatalf("add phone: %v", err)
	}
	if err := store.UpdateSubscriptions(ctx, "alice", []string{"http://shared.com/feed"}, nil, 50); err != nil {
		t.Fatalf("add laptop: %v", err)
	}

	t.Run("removing subscription removes it for the user", func(t *testing.T) {
		if err := store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://shared.com/feed"}, 200); err != nil {
			t.Fatalf("remove subscription: %v", err)
		}

		phoneSubs, _ := store.GetSubscriptions(ctx, "alice")
		laptopSubs, _ := store.GetSubscriptions(ctx, "alice")

		if len(phoneSubs) != 0 {
			t.Errorf("phone should have 0 active subs, got %d", len(phoneSubs))
		}
		if len(laptopSubs) != 0 {
			t.Errorf("laptop should have 0 active subs, got %d", len(laptopSubs))
		}
	})

	t.Run("removal shows in changes feed for other device", func(t *testing.T) {
		changes, err := store.GetSubscriptionChanges(ctx, "alice", 100)
		if err != nil {
			t.Fatalf("GetSubscriptionChanges: %v", err)
		}
		var found bool
		for _, url := range changes.Remove {
			if url == "http://shared.com/feed" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected http://shared.com/feed in remove list, got add=%v remove=%v", changes.Add, changes.Remove)
		}
	})
}

func TestSQLiteStore_DeviceLastActivity(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	caption := "Phone"
	typ := "mobile"
	if err := store.UpsertDevice(ctx, "alice", "phone1", DeviceUpdate{Caption: &caption, Type: &typ}); err != nil {
		t.Fatalf("UpsertDevice: %v", err)
	}

	devices, _ := store.ListDevices(ctx, "alice")
	if devices[0].LastActivity != nil {
		t.Error("expected nil LastActivity initially")
	}

	now := time.Now()
	if err := store.UpdateDeviceLastActivity(ctx, "alice", "phone1", now); err != nil {
		t.Fatalf("UpdateDeviceLastActivity: %v", err)
	}

	devices, _ = store.ListDevices(ctx, "alice")
	if devices[0].LastActivity == nil {
		t.Fatal("expected non-nil LastActivity after update")
	}
	if devices[0].LastActivity.Unix() != now.Unix() {
		t.Errorf("LastActivity = %v, want %v", devices[0].LastActivity.Unix(), now.Unix())
	}
}

func TestSQLiteStore_FullSyncWorkflow(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Step 1: Web UI adds 5 subscriptions (using ReactivateSubscription + UpdateSubscriptions)
	webFeeds := []string{"http://a.com", "http://b.com", "http://c.com", "http://d.com", "http://e.com"}
	for _, u := range webFeeds {
		_ = store.ReactivateSubscription(ctx, "alice", u, 100)
	}
	_ = store.UpdateSubscriptions(ctx, "alice", webFeeds, nil, 100)

	subs, _ := store.GetSubscriptions(ctx, "alice")
	if len(subs) != 5 {
		t.Fatalf("step 1: expected 5 subs, got %d", len(subs))
	}

	// Step 2: AntennaPod first sync (since=0) — gets all 5
	changes, _ := store.GetSubscriptionChanges(ctx, "alice", 0)
	if len(changes.Add) != 5 {
		t.Fatalf("step 2: expected 5 adds, got %d", len(changes.Add))
	}
	if len(changes.Remove) != 0 {
		t.Fatalf("step 2: expected 0 removes, got %d", len(changes.Remove))
	}

	// Step 3: AntennaPod echoes back all 5 as add (normal client behavior after receiving)
	_ = store.UpdateSubscriptions(ctx, "alice", webFeeds, nil, 200)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 5 {
		t.Fatalf("step 3: expected still 5 subs, got %d", len(subs))
	}

	// Step 4: User deletes 2 in AntennaPod, app sends remove
	_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://b.com", "http://c.com"}, 300)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 3 {
		t.Fatalf("step 4: expected 3 subs after delete, got %d: %v", len(subs), subs)
	}

	// Step 5: Re-adding only active feeds doesn't reactivate deleted ones
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com", "http://d.com", "http://e.com"}, nil, 400)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 3 {
		t.Fatalf("step 5: expected 3 subs, got %d: %v", len(subs), subs)
	}

	// Step 5b: But explicitly re-adding a deleted feed DOES reactivate it
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://b.com"}, nil, 450)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 4 {
		t.Fatalf("step 5b: expected 4 subs after re-add of b.com, got %d: %v", len(subs), subs)
	}
	// Delete it again for next steps
	_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://b.com"}, 460)

	// Step 6: Changes feed with since=461 shows nothing (all deletions happened before 461)
	changes, _ = store.GetSubscriptionChanges(ctx, "alice", 461)
	if len(changes.Remove) != 0 {
		t.Errorf("step 6: expected 0 removes with since=301, got %v", changes.Remove)
	}

	// Step 7: Web UI can still re-add a deleted feed
	_ = store.ReactivateSubscription(ctx, "alice", "http://b.com", 500)
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://b.com"}, nil, 500)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 4 {
		t.Fatalf("step 7: expected 4 subs after web UI re-add, got %d: %v", len(subs), subs)
	}

	// Step 8: PUT (full replace) removes feeds not in the list
	_ = store.ReplaceSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com"}, 600)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 2 {
		t.Fatalf("step 8: expected 2 subs after PUT replace, got %d: %v", len(subs), subs)
	}

	// Step 9: Changes feed correctly shows the removals from PUT
	changes, _ = store.GetSubscriptionChanges(ctx, "alice", 550)
	if len(changes.Remove) != 2 {
		t.Errorf("step 9: expected 2 removes after PUT, got %v", changes.Remove)
	}

	// Step 10: Verify no ghost subscriptions — only active ones remain
	subs, _ = store.GetSubscriptions(ctx, "alice")
	for _, s := range subs {
		if s != "http://a.com" && s != "http://b.com" {
			t.Errorf("step 10: unexpected active subscription %q", s)
		}
	}
}

func TestSQLiteStore_MultiDeviceRealWorld(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Device 1 connects with 3 local podcasts
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://phone1.com", "http://phone2.com", "http://phone3.com"}, nil, 100)

	// Device 2 connects with 2 different podcasts
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://tablet1.com", "http://tablet2.com"}, nil, 200)

	// Both devices should now see all 5
	subs, _ := store.GetSubscriptions(ctx, "alice")
	if len(subs) != 5 {
		t.Fatalf("expected 5 subs after both devices sync, got %d: %v", len(subs), subs)
	}

	// Device 1 syncs and gets device 2's feeds
	changes, _ := store.GetSubscriptionChanges(ctx, "alice", 101)
	if len(changes.Add) != 2 {
		t.Errorf("device 1 should see 2 new feeds from device 2, got %d: %v", len(changes.Add), changes.Add)
	}

	// Device 1 deletes one of device 2's feeds
	_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://tablet1.com"}, 300)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 4 {
		t.Fatalf("expected 4 subs after device 1 deletes tablet1, got %d: %v", len(subs), subs)
	}

	// Device 2 syncs — sees tablet1 in remove
	changes, _ = store.GetSubscriptionChanges(ctx, "alice", 201)
	var foundRemove bool
	for _, u := range changes.Remove {
		if u == "http://tablet1.com" {
			foundRemove = true
		}
	}
	if !foundRemove {
		t.Errorf("device 2 should see tablet1 in remove, got add=%v remove=%v", changes.Add, changes.Remove)
	}

	// Device 1 re-subscribes to tablet1 (genuine re-subscribe)
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://tablet1.com"}, nil, 400)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 5 {
		t.Fatalf("expected 5 subs after re-subscribe, got %d: %v", len(subs), subs)
	}

	// Device 2 syncs again — sees tablet1 back in add
	changes, _ = store.GetSubscriptionChanges(ctx, "alice", 301)
	var foundAdd bool
	for _, u := range changes.Add {
		if u == "http://tablet1.com" {
			foundAdd = true
		}
	}
	if !foundAdd {
		t.Errorf("device 2 should see tablet1 re-added, got add=%v remove=%v", changes.Add, changes.Remove)
	}

	// Device 2 deletes ALL its feeds and some of device 1's
	_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://tablet1.com", "http://tablet2.com", "http://phone1.com"}, 500)
	subs, _ = store.GetSubscriptions(ctx, "alice")
	if len(subs) != 2 {
		t.Fatalf("expected 2 subs after bulk delete, got %d: %v", len(subs), subs)
	}

	// Device 1 syncs — sees 3 removals
	changes, _ = store.GetSubscriptionChanges(ctx, "alice", 401)
	if len(changes.Remove) != 3 {
		t.Errorf("device 1 should see 3 removals, got %v", changes.Remove)
	}

	// Final state: only phone2 and phone3 remain
	subs, _ = store.GetSubscriptions(ctx, "alice")
	expected := map[string]bool{"http://phone2.com": true, "http://phone3.com": true}
	for _, s := range subs {
		if !expected[s] {
			t.Errorf("unexpected subscription %q in final state", s)
		}
	}
}

func TestSQLiteStore_SyncEdgeCases(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	t.Run("delete then re-add reactivates subscription", func(t *testing.T) {
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://ghost.com"}, nil, 100)
		_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://ghost.com"}, 200)
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://ghost.com"}, nil, 300)

		subs, _ := store.GetSubscriptions(ctx, "alice")
		var found bool
		for _, s := range subs {
			if s == "http://ghost.com" {
				found = true
			}
		}
		if !found {
			t.Error("re-add after delete should reactivate subscription")
		}
		// cleanup
		_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://ghost.com"}, 350)
	})

	t.Run("simultaneous add and remove of different feeds in one call", func(t *testing.T) {
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://x.com"}, nil, 400)
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://y.com"}, []string{"http://x.com"}, 500)

		subs, _ := store.GetSubscriptions(ctx, "alice")
		for _, s := range subs {
			if s == "http://x.com" {
				t.Error("x.com should be removed")
			}
		}
		var foundY bool
		for _, s := range subs {
			if s == "http://y.com" {
				foundY = true
			}
		}
		if !foundY {
			t.Error("y.com should be active")
		}
	})

	t.Run("PUT with empty list removes everything", func(t *testing.T) {
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://1.com", "http://2.com"}, nil, 600)
		_ = store.ReplaceSubscriptions(ctx, "alice", []string{}, 700)

		subs, _ := store.GetSubscriptions(ctx, "alice")
		if len(subs) != 0 {
			t.Errorf("PUT with empty list should remove all, got %v", subs)
		}
	})

	t.Run("PUT with nil list removes everything", func(t *testing.T) {
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://3.com"}, nil, 800)
		_ = store.ReplaceSubscriptions(ctx, "alice", nil, 900)

		subs, _ := store.GetSubscriptions(ctx, "alice")
		if len(subs) != 0 {
			t.Errorf("PUT with nil list should remove all, got %v", subs)
		}
	})

	t.Run("changes feed with since=0 on empty user returns nothing", func(t *testing.T) {
		if err := store.CreateUser(ctx, "bob", "hash", testAccountID); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		changes, _ := store.GetSubscriptionChanges(ctx, "bob", 0)
		if len(changes.Add) != 0 || len(changes.Remove) != 0 {
			t.Errorf("expected empty changes for new user, got add=%v remove=%v", changes.Add, changes.Remove)
		}
	})

	t.Run("double delete is idempotent", func(t *testing.T) {
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://dup.com"}, nil, 1000)
		_ = store.ReactivateSubscription(ctx, "alice", "http://dup.com", 1000)
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://dup.com"}, nil, 1000)
		_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://dup.com"}, 1100)
		_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://dup.com"}, 1200)

		subs, _ := store.GetSubscriptions(ctx, "alice")
		for _, s := range subs {
			if s == "http://dup.com" {
				t.Error("dup.com should be deleted after double delete")
			}
		}
	})

	t.Run("rapid add-delete-add-delete cycle leaves subscription deleted", func(t *testing.T) {
		for i := range 5 {
			ts := int64(2000 + i*2)
			_ = store.ReactivateSubscription(ctx, "alice", "http://flip.com", ts)
			_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://flip.com"}, nil, ts)
			_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://flip.com"}, ts+1)
		}

		subs, _ := store.GetSubscriptions(ctx, "alice")
		for _, s := range subs {
			if s == "http://flip.com" {
				t.Error("flip.com should be deleted after rapid flip cycle")
			}
		}
	})

	t.Run("changes feed returns correct state after many operations", func(t *testing.T) {
		_ = store.ReactivateSubscription(ctx, "alice", "http://final.com", 3000)
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://final.com"}, nil, 3000)
		_ = store.UpdateSubscriptions(ctx, "alice", nil, []string{"http://final.com"}, 3100)
		_ = store.ReactivateSubscription(ctx, "alice", "http://final.com", 3200)
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://final.com"}, nil, 3200)

		changes, _ := store.GetSubscriptionChanges(ctx, "alice", 3050)
		var inAdd, inRemove bool
		for _, u := range changes.Add {
			if u == "http://final.com" {
				inAdd = true
			}
		}
		for _, u := range changes.Remove {
			if u == "http://final.com" {
				inRemove = true
			}
		}
		if !inAdd {
			t.Error("final.com should be in add (reactivated at 3200)")
		}
		if inRemove {
			t.Error("final.com should NOT be in remove (final state is active)")
		}
	})

	t.Run("ReplaceSubscriptions with exact same list is no-op", func(t *testing.T) {
		_ = store.ReactivateSubscription(ctx, "alice", "http://stable.com", 4000)
		_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://stable.com"}, nil, 4000)

		_ = store.ReplaceSubscriptions(ctx, "alice", []string{"http://stable.com"}, 4100)

		changes, _ := store.GetSubscriptionChanges(ctx, "alice", 4050)
		for _, u := range changes.Add {
			if u == "http://stable.com" {
				t.Error("replacing with same list should not generate a change")
			}
		}
		for _, u := range changes.Remove {
			if u == "http://stable.com" {
				t.Error("replacing with same list should not generate a removal")
			}
		}
	})
}

func TestNullHelpers(t *testing.T) {
	t.Run("nullStringPtr valid", func(t *testing.T) {
		ns := sql.NullString{String: "hello", Valid: true}
		p := nullStringPtr(ns)
		if p == nil || *p != "hello" {
			t.Errorf("got %v, want ptr to 'hello'", p)
		}
	})

	t.Run("nullStringPtr invalid", func(t *testing.T) {
		ns := sql.NullString{}
		p := nullStringPtr(ns)
		if p != nil {
			t.Errorf("got %v, want nil", p)
		}
	})

	t.Run("nullInt64Ptr valid", func(t *testing.T) {
		ni := sql.NullInt64{Int64: 42, Valid: true}
		p := nullInt64Ptr(ni)
		if p == nil || *p != 42 {
			t.Errorf("got %v, want ptr to 42", p)
		}
	})

	t.Run("nullInt64Ptr invalid", func(t *testing.T) {
		ni := sql.NullInt64{}
		p := nullInt64Ptr(ni)
		if p != nil {
			t.Errorf("got %v, want nil", p)
		}
	})

	t.Run("ptrToNullString non-nil", func(t *testing.T) {
		s := "hello"
		ns := ptrToNullString(&s)
		if !ns.Valid || ns.String != "hello" {
			t.Errorf("got %v, want valid 'hello'", ns)
		}
	})

	t.Run("ptrToNullString nil", func(t *testing.T) {
		ns := ptrToNullString(nil)
		if ns.Valid {
			t.Errorf("got valid, want invalid")
		}
	})

	t.Run("ptrToNullInt64 non-nil", func(t *testing.T) {
		i := int64(99)
		ni := ptrToNullInt64(&i)
		if !ni.Valid || ni.Int64 != 99 {
			t.Errorf("got %v, want valid 99", ni)
		}
	})

	t.Run("ptrToNullInt64 nil", func(t *testing.T) {
		ni := ptrToNullInt64(nil)
		if ni.Valid {
			t.Errorf("got valid, want invalid")
		}
	})

	t.Run("ptrStringOr non-nil", func(t *testing.T) {
		s := "value"
		got := ptrStringOr(&s, "default")
		if got != "value" {
			t.Errorf("got %q, want %q", got, "value")
		}
	})

	t.Run("ptrStringOr nil", func(t *testing.T) {
		got := ptrStringOr(nil, "default")
		if got != "default" {
			t.Errorf("got %q, want %q", got, "default")
		}
	})

	t.Run("nullStringVal valid", func(t *testing.T) {
		ns := sql.NullString{String: "hello", Valid: true}
		if got := nullStringVal(ns); got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})

	t.Run("nullStringVal invalid", func(t *testing.T) {
		ns := sql.NullString{}
		if got := nullStringVal(ns); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestISOTimestampConversion(t *testing.T) {
	tests := []struct {
		name    string
		input   *string
		wantNil bool
	}{
		{"nil", nil, true},
		{"empty", new(""), true},
		{"invalid format", new("not-a-date"), true},
		{"valid ISO", new("2024-06-01T14:30:00"), false},
		{"midnight", new("2024-01-01T00:00:00"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ni := isoTimestampToNullInt64(tt.input)
			if tt.wantNil && ni.Valid {
				t.Errorf("expected invalid NullInt64, got valid with %d", ni.Int64)
			}
			if !tt.wantNil && !ni.Valid {
				t.Error("expected valid NullInt64, got invalid")
			}
		})
	}

	t.Run("roundtrip", func(t *testing.T) {
		input := "2024-06-15T10:30:00"
		ni := isoTimestampToNullInt64(&input)
		if !ni.Valid {
			t.Fatal("expected valid")
		}
		back := nullInt64ToISO(ni)
		if back == nil {
			t.Fatal("expected non-nil")
		}
		if *back != input {
			t.Errorf("roundtrip: got %q, want %q", *back, input)
		}
	})

	t.Run("nullInt64ToISO invalid", func(t *testing.T) {
		ni := sql.NullInt64{}
		got := nullInt64ToISO(ni)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

func TestEpisodeHash(t *testing.T) {
	t.Run("same input same hash", func(t *testing.T) {
		pos := int64(120)
		ep := Episode{Action: "play", Position: &pos}
		a := episodeHash(ep)
		b := episodeHash(ep)
		if a != b {
			t.Errorf("same input should produce same hash")
		}
	})

	t.Run("different action different hash", func(t *testing.T) {
		a := episodeHash(Episode{Action: "play"})
		b := episodeHash(Episode{Action: "download"})
		if a == b {
			t.Error("different actions should produce different hashes")
		}
	})

	t.Run("different position different hash", func(t *testing.T) {
		pos1 := int64(100)
		pos2 := int64(200)
		a := episodeHash(Episode{Action: "play", Position: &pos1})
		b := episodeHash(Episode{Action: "play", Position: &pos2})
		if a == b {
			t.Error("different positions should produce different hashes")
		}
	})

	t.Run("nil fields handled", func(t *testing.T) {
		h := episodeHash(Episode{Action: "new"})
		if len(h) != 64 {
			t.Errorf("hash length = %d, want 64", len(h))
		}
	})
}

func TestSQLiteStore_Settings(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	t.Run("get nonexistent setting returns error", func(t *testing.T) {
		_, err := store.GetSetting(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent setting")
		}
	})

	t.Run("set and get setting", func(t *testing.T) {
		if err := store.SetSetting(ctx, "self_registration", "true"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		val, err := store.GetSetting(ctx, "self_registration")
		if err != nil {
			t.Fatalf("GetSetting: %v", err)
		}
		if val != "true" {
			t.Errorf("value = %q, want true", val)
		}
	})

	t.Run("upsert overwrites existing setting", func(t *testing.T) {
		if err := store.SetSetting(ctx, "self_registration", "false"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		val, err := store.GetSetting(ctx, "self_registration")
		if err != nil {
			t.Fatalf("GetSetting: %v", err)
		}
		if val != "false" {
			t.Errorf("value = %q, want false", val)
		}
	})

	t.Run("multiple settings are independent", func(t *testing.T) {
		if err := store.SetSetting(ctx, "allow_user_creation", "true"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		v1, _ := store.GetSetting(ctx, "self_registration")
		v2, _ := store.GetSetting(ctx, "allow_user_creation")
		if v1 != "false" {
			t.Errorf("self_registration = %q, want false", v1)
		}
		if v2 != "true" {
			t.Errorf("allow_user_creation = %q, want true", v2)
		}
	})
}

func TestSQLiteStore_ListUsersByAccountWithStats(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", "hash", testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_ = store.UpsertDevice(ctx, "alice", "phone", DeviceUpdate{})
	_ = store.UpsertDevice(ctx, "alice", "laptop", DeviceUpdate{})
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com", "http://b.com"}, nil, 100)

	users, err := store.ListUsersByAccountWithStats(ctx, testAccountID)
	if err != nil {
		t.Fatalf("ListUsersByAccountWithStats: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("got %d users, want 1", len(users))
	}
	if users[0].Devices != 2 {
		t.Errorf("devices = %d, want 2", users[0].Devices)
	}
	if users[0].Subscriptions != 2 {
		t.Errorf("subscriptions = %d, want 2", users[0].Subscriptions)
	}
}

func TestSQLiteStore_ShareToken(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", hashPassword("pass"), testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user, _ := store.GetUser(ctx, "alice")
	if user.ShareToken != nil {
		t.Fatal("expected nil ShareToken initially")
	}

	token := "share-token-abc"
	if err := store.SetUserShareToken(ctx, "alice", &token); err != nil {
		t.Fatalf("SetUserShareToken: %v", err)
	}

	user, _ = store.GetUser(ctx, "alice")
	if user.ShareToken == nil || *user.ShareToken != token {
		t.Fatalf("expected ShareToken = %q, got %v", token, user.ShareToken)
	}

	found, err := store.GetUserByShareToken(ctx, token)
	if err != nil {
		t.Fatalf("GetUserByShareToken: %v", err)
	}
	if found.Username != "alice" {
		t.Errorf("expected username alice, got %q", found.Username)
	}

	_, err = store.GetUserByShareToken(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent token")
	}

	if err := store.SetUserShareToken(ctx, "alice", nil); err != nil {
		t.Fatalf("SetUserShareToken(nil): %v", err)
	}

	user, _ = store.GetUser(ctx, "alice")
	if user.ShareToken != nil {
		t.Error("expected nil ShareToken after disabling")
	}
}

func TestSQLiteStore_ListInactiveAccounts(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	now := time.Now()
	cutoff := now.AddDate(0, 0, -30).Unix()

	activeAcctID := "active-acct"
	if err := store.CreateAccount(ctx, activeAcctID, "active", "hash", RoleStandard, now.AddDate(0, -1, 0)); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if err := store.CreateUser(ctx, "activeuser", "hash", activeAcctID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.UpdateUserLastActivity(ctx, "activeuser", now.AddDate(0, 0, -5)); err != nil {
		t.Fatalf("UpdateUserLastActivity: %v", err)
	}

	inactiveAcctID := "inactive-acct"
	if err := store.CreateAccount(ctx, inactiveAcctID, "inactive", "hash", RoleStandard, now.AddDate(0, -6, 0)); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if err := store.CreateUser(ctx, "inactiveuser", "hash", inactiveAcctID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.UpdateUserLastActivity(ctx, "inactiveuser", now.AddDate(0, -3, 0)); err != nil {
		t.Fatalf("UpdateUserLastActivity: %v", err)
	}

	adminAcctID := "admin-old"
	if err := store.CreateAccount(ctx, adminAcctID, "oldadmin", "hash", RoleAdmin, now.AddDate(-1, 0, 0)); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	noUserAcctID := "no-user-acct"
	if err := store.CreateAccount(ctx, noUserAcctID, "nouser", "hash", RoleStandard, now.AddDate(0, -3, 0)); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	accounts, err := store.ListInactiveAccounts(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListInactiveAccounts: %v", err)
	}

	inactive := make(map[string]bool)
	for _, a := range accounts {
		inactive[a.Username] = true
	}

	if inactive["active"] {
		t.Error("active account (activity 5 days ago) should not be listed as inactive")
	}
	if !inactive["inactive"] {
		t.Error("inactive account (activity 3 months ago) should be listed as inactive")
	}
	if inactive["oldadmin"] {
		t.Error("admin account should never be listed as inactive")
	}
	if inactive["testaccount"] {
		t.Error("the test fixture admin account should not be listed")
	}
	if !inactive["nouser"] {
		t.Error("account with no users (created 3 months ago) should be listed as inactive")
	}
}

func TestSQLiteStore_DeleteEpisodesOlderThan(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	if err := store.CreateUser(ctx, "alice", hashPassword("pass"), testAccountID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	now := time.Now().Unix()
	old := now - 100*24*60*60 // 100 days ago

	episodes := []Episode{
		{Podcast: "http://old.com/feed", Episode: "http://old.com/ep1", Action: "play"},
		{Podcast: "http://new.com/feed", Episode: "http://new.com/ep1", Action: "play"},
	}
	if err := store.UpdateEpisodes(ctx, "alice", episodes[:1], old); err != nil {
		t.Fatalf("UpdateEpisodes (old): %v", err)
	}
	if err := store.UpdateEpisodes(ctx, "alice", episodes[1:], now); err != nil {
		t.Fatalf("UpdateEpisodes (new): %v", err)
	}

	cutoff := now - 90*24*60*60
	n, err := store.DeleteEpisodesOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteEpisodesOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}

	remaining, _ := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Since: 0})
	if len(remaining) != 1 {
		t.Fatalf("remaining episodes = %d, want 1", len(remaining))
	}
	if remaining[0].Episode != "http://new.com/ep1" {
		t.Errorf("remaining episode = %q, want http://new.com/ep1", remaining[0].Episode)
	}
}

func TestSQLiteStore_APIKeys(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	t.Run("create and list", func(t *testing.T) {
		key := APIKey{
			ID:        "key-1",
			AccountID: testAccountID,
			Name:      "test key",
			Prefix:    "gp_aabbccdd",
			Hash:      "bcrypt-hash-here",
			Role:      RoleStandard,
			CreatedAt: time.Now(),
		}
		if err := store.CreateAPIKey(ctx, key); err != nil {
			t.Fatalf("CreateAPIKey: %v", err)
		}

		keys, err := store.ListAPIKeysByAccount(ctx, testAccountID)
		if err != nil {
			t.Fatalf("ListAPIKeysByAccount: %v", err)
		}
		if len(keys) != 1 {
			t.Fatalf("expected 1 key, got %d", len(keys))
		}
		if keys[0].ID != "key-1" {
			t.Errorf("ID = %q, want %q", keys[0].ID, "key-1")
		}
		if keys[0].Name != "test key" {
			t.Errorf("Name = %q, want %q", keys[0].Name, "test key")
		}
		if keys[0].Role != RoleStandard {
			t.Errorf("Role = %q, want %q", keys[0].Role, RoleStandard)
		}
		if keys[0].Hash != "" {
			t.Error("ListAPIKeysByAccount should NOT return hash")
		}
	})

	t.Run("get by prefix", func(t *testing.T) {
		keys, err := store.GetAPIKeysByPrefix(ctx, "gp_aabbccdd")
		if err != nil {
			t.Fatalf("GetAPIKeysByPrefix: %v", err)
		}
		if len(keys) != 1 {
			t.Fatalf("expected 1 key, got %d", len(keys))
		}
		if keys[0].Hash != "bcrypt-hash-here" {
			t.Errorf("GetAPIKeysByPrefix should return hash, got %q", keys[0].Hash)
		}
	})

	t.Run("get by prefix not found", func(t *testing.T) {
		keys, err := store.GetAPIKeysByPrefix(ctx, "gp_zzzzzzzz")
		if err != nil {
			t.Fatalf("GetAPIKeysByPrefix: %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(keys))
		}
	})

	t.Run("count", func(t *testing.T) {
		count, err := store.CountAPIKeysByAccount(ctx, testAccountID)
		if err != nil {
			t.Fatalf("CountAPIKeysByAccount: %v", err)
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
	})

	t.Run("update last used", func(t *testing.T) {
		now := time.Now()
		if err := store.UpdateAPIKeyLastUsed(ctx, "key-1", now); err != nil {
			t.Fatalf("UpdateAPIKeyLastUsed: %v", err)
		}
		keys, _ := store.ListAPIKeysByAccount(ctx, testAccountID)
		if keys[0].LastUsed == nil {
			t.Fatal("LastUsed should be set after update")
		}
		if keys[0].LastUsed.Unix() != now.Unix() {
			t.Errorf("LastUsed = %v, want %v", keys[0].LastUsed.Unix(), now.Unix())
		}
	})

	t.Run("delete specific key", func(t *testing.T) {
		key2 := APIKey{
			ID:        "key-2",
			AccountID: testAccountID,
			Name:      "second key",
			Prefix:    "gp_11223344",
			Hash:      "hash2",
			Role:      RoleAdmin,
			CreatedAt: time.Now(),
		}
		if err := store.CreateAPIKey(ctx, key2); err != nil {
			t.Fatalf("CreateAPIKey: %v", err)
		}

		if err := store.DeleteAPIKey(ctx, "key-1", testAccountID); err != nil {
			t.Fatalf("DeleteAPIKey: %v", err)
		}
		keys, _ := store.ListAPIKeysByAccount(ctx, testAccountID)
		if len(keys) != 1 {
			t.Fatalf("expected 1 key after delete, got %d", len(keys))
		}
		if keys[0].ID != "key-2" {
			t.Errorf("remaining key ID = %q, want %q", keys[0].ID, "key-2")
		}
	})

	t.Run("delete key wrong account", func(t *testing.T) {
		if err := store.DeleteAPIKey(ctx, "key-2", "wrong-account"); err != nil {
			t.Fatalf("DeleteAPIKey: %v", err)
		}
		keys, _ := store.ListAPIKeysByAccount(ctx, testAccountID)
		if len(keys) != 1 {
			t.Error("key should NOT have been deleted when account ID doesn't match")
		}
	})

	t.Run("delete all by account", func(t *testing.T) {
		if err := store.DeleteAPIKeysByAccount(ctx, testAccountID); err != nil {
			t.Fatalf("DeleteAPIKeysByAccount: %v", err)
		}
		keys, _ := store.ListAPIKeysByAccount(ctx, testAccountID)
		if len(keys) != 0 {
			t.Errorf("expected 0 keys after bulk delete, got %d", len(keys))
		}
	})

	t.Run("list empty account", func(t *testing.T) {
		keys, err := store.ListAPIKeysByAccount(ctx, "nonexistent-account")
		if err != nil {
			t.Fatalf("ListAPIKeysByAccount: %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("expected 0 keys for nonexistent account, got %d", len(keys))
		}
	})
}

func TestSQLiteStore_Accounts(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	t.Run("get by username", func(t *testing.T) {
		acct, err := store.GetAccount(ctx, "testaccount")
		if err != nil {
			t.Fatalf("GetAccount: %v", err)
		}
		if acct.ID != testAccountID {
			t.Errorf("ID = %q, want %q", acct.ID, testAccountID)
		}
		if acct.Role != RoleAdmin {
			t.Errorf("Role = %q, want %q", acct.Role, RoleAdmin)
		}
	})

	t.Run("get by username not found", func(t *testing.T) {
		_, err := store.GetAccount(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent account")
		}
	})

	t.Run("get by ID", func(t *testing.T) {
		acct, err := store.GetAccountByID(ctx, testAccountID)
		if err != nil {
			t.Fatalf("GetAccountByID: %v", err)
		}
		if acct.Username != "testaccount" {
			t.Errorf("Username = %q, want %q", acct.Username, "testaccount")
		}
	})

	t.Run("get by ID not found", func(t *testing.T) {
		_, err := store.GetAccountByID(ctx, "no-such-id")
		if err == nil {
			t.Fatal("expected error for nonexistent ID")
		}
	})

	t.Run("count", func(t *testing.T) {
		count, err := store.CountAccounts(ctx)
		if err != nil {
			t.Fatalf("CountAccounts: %v", err)
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
	})

	t.Run("update username", func(t *testing.T) {
		if err := store.UpdateAccountUsername(ctx, testAccountID, "renamed"); err != nil {
			t.Fatalf("UpdateAccountUsername: %v", err)
		}
		acct, _ := store.GetAccountByID(ctx, testAccountID)
		if acct.Username != "renamed" {
			t.Errorf("Username = %q, want %q", acct.Username, "renamed")
		}
		_ = store.UpdateAccountUsername(ctx, testAccountID, "testaccount")
	})

	t.Run("update password", func(t *testing.T) {
		if err := store.UpdateAccountPassword(ctx, testAccountID, "newhash"); err != nil {
			t.Fatalf("UpdateAccountPassword: %v", err)
		}
		acct, _ := store.GetAccountByID(ctx, testAccountID)
		if acct.PWHash != "newhash" {
			t.Errorf("PWHash = %q, want %q", acct.PWHash, "newhash")
		}
	})

	t.Run("update role", func(t *testing.T) {
		if err := store.UpdateAccountRole(ctx, testAccountID, RoleStandard); err != nil {
			t.Fatalf("UpdateAccountRole: %v", err)
		}
		acct, _ := store.GetAccountByID(ctx, testAccountID)
		if acct.Role != RoleStandard {
			t.Errorf("Role = %q, want %q", acct.Role, RoleStandard)
		}
		_ = store.UpdateAccountRole(ctx, testAccountID, RoleAdmin)
	})

	t.Run("session lifecycle", func(t *testing.T) {
		sid := "web-session-abc"
		now := time.Now()
		if err := store.UpdateAccountSession(ctx, testAccountID, &sid, now); err != nil {
			t.Fatalf("UpdateAccountSession: %v", err)
		}

		acct, err := store.GetAccountBySession(ctx, sid)
		if err != nil {
			t.Fatalf("GetAccountBySession: %v", err)
		}
		if acct.ID != testAccountID {
			t.Errorf("ID = %q, want %q", acct.ID, testAccountID)
		}

		if err := store.UpdateAccountSession(ctx, testAccountID, nil, time.Time{}); err != nil {
			t.Fatalf("UpdateAccountSession (clear): %v", err)
		}
		_, err = store.GetAccountBySession(ctx, sid)
		if err == nil {
			t.Error("expected error after session cleared")
		}
	})

	t.Run("last login", func(t *testing.T) {
		now := time.Now()
		if err := store.UpdateAccountLastLogin(ctx, testAccountID, now); err != nil {
			t.Fatalf("UpdateAccountLastLogin: %v", err)
		}
	})

	t.Run("list accounts", func(t *testing.T) {
		_ = store.CreateAccount(ctx, "second-id", "second", "hash", RoleStandard, time.Now())
		accounts, err := store.ListAccounts(ctx)
		if err != nil {
			t.Fatalf("ListAccounts: %v", err)
		}
		if len(accounts) != 2 {
			t.Fatalf("expected 2 accounts, got %d", len(accounts))
		}
	})

	t.Run("delete account", func(t *testing.T) {
		if err := store.DeleteAccount(ctx, "second-id"); err != nil {
			t.Fatalf("DeleteAccount: %v", err)
		}
		count, _ := store.CountAccounts(ctx)
		if count != 1 {
			t.Errorf("count after delete = %d, want 1", count)
		}
	})

	t.Run("ping", func(t *testing.T) {
		if err := store.Ping(ctx); err != nil {
			t.Errorf("Ping: %v", err)
		}
	})
}

func TestSQLiteStore_UsersCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	_ = store.CreateUser(ctx, "alice", hashPassword("pass"), testAccountID)
	_ = store.CreateUser(ctx, "bob", hashPassword("pass"), testAccountID)

	t.Run("list all users", func(t *testing.T) {
		users, err := store.ListUsers(ctx)
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 2 {
			t.Errorf("expected 2 users, got %d", len(users))
		}
	})

	t.Run("list users by account", func(t *testing.T) {
		users, err := store.ListUsersByAccount(ctx, testAccountID)
		if err != nil {
			t.Fatalf("ListUsersByAccount: %v", err)
		}
		if len(users) != 2 {
			t.Errorf("expected 2 users, got %d", len(users))
		}
	})

	t.Run("list users by account empty", func(t *testing.T) {
		users, err := store.ListUsersByAccount(ctx, "no-such-account")
		if err != nil {
			t.Fatalf("ListUsersByAccount: %v", err)
		}
		if len(users) != 0 {
			t.Errorf("expected 0 users, got %d", len(users))
		}
	})

	t.Run("update password", func(t *testing.T) {
		if err := store.UpdateUserPassword(ctx, "alice", "newhash"); err != nil {
			t.Fatalf("UpdateUserPassword: %v", err)
		}
		user, _ := store.GetUser(ctx, "alice")
		if user.PWHash != "newhash" {
			t.Errorf("PWHash = %q, want %q", user.PWHash, "newhash")
		}
	})

	t.Run("delete user", func(t *testing.T) {
		if err := store.DeleteUser(ctx, "bob"); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		_, err := store.GetUser(ctx, "bob")
		if err == nil {
			t.Error("expected error for deleted user")
		}
	})

	t.Run("delete users by account", func(t *testing.T) {
		_ = store.CreateUser(ctx, "temp1", "h", testAccountID)
		_ = store.CreateUser(ctx, "temp2", "h", testAccountID)
		if err := store.DeleteUsersByAccount(ctx, testAccountID); err != nil {
			t.Fatalf("DeleteUsersByAccount: %v", err)
		}
		users, _ := store.ListUsersByAccount(ctx, testAccountID)
		if len(users) != 0 {
			t.Errorf("expected 0 users, got %d", len(users))
		}
	})
}

func TestSQLiteStore_CascadeDeletes(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	_ = store.CreateUser(ctx, "alice", "hash", testAccountID)
	_ = store.UpsertDevice(ctx, "alice", "phone", DeviceUpdate{})
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com"}, nil, time.Now().Unix())
	_ = store.UpdateEpisodes(ctx, "alice", []Episode{{Podcast: "http://a.com", Episode: "http://a.com/1", Action: "play"}}, time.Now().Unix())

	t.Run("delete all user devices", func(t *testing.T) {
		_ = store.UpsertDevice(ctx, "alice", "tablet", DeviceUpdate{})
		if err := store.DeleteAllUserDevices(ctx, "alice"); err != nil {
			t.Fatalf("DeleteAllUserDevices: %v", err)
		}
		devices, _ := store.ListDevices(ctx, "alice")
		if len(devices) != 0 {
			t.Errorf("expected 0 devices, got %d", len(devices))
		}
	})

	t.Run("delete all user subscriptions", func(t *testing.T) {
		if err := store.DeleteAllUserSubscriptions(ctx, "alice"); err != nil {
			t.Fatalf("DeleteAllUserSubscriptions: %v", err)
		}
		subs, _ := store.GetSubscriptions(ctx, "alice")
		if len(subs) != 0 {
			t.Errorf("expected 0 subs, got %d", len(subs))
		}
	})

	t.Run("delete all user episodes", func(t *testing.T) {
		if err := store.DeleteAllUserEpisodes(ctx, "alice"); err != nil {
			t.Fatalf("DeleteAllUserEpisodes: %v", err)
		}
		eps, _ := store.GetEpisodes(ctx, EpisodeQuery{Username: "alice", Since: 0})
		if len(eps) != 0 {
			t.Errorf("expected 0 episodes, got %d", len(eps))
		}
	})
}

func TestSQLiteStore_GetStats(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	_ = store.CreateUser(ctx, "alice", "hash", testAccountID)
	_ = store.UpsertDevice(ctx, "alice", "phone", DeviceUpdate{})
	_ = store.UpdateSubscriptions(ctx, "alice", []string{"http://a.com"}, nil, time.Now().Unix())

	stats, err := store.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.Accounts != 1 {
		t.Errorf("Accounts = %d, want 1", stats.Accounts)
	}
	if stats.Users != 1 {
		t.Errorf("Users = %d, want 1", stats.Users)
	}
	if stats.Devices != 1 {
		t.Errorf("Devices = %d, want 1", stats.Devices)
	}
	if stats.Subscriptions != 1 {
		t.Errorf("Subscriptions = %d, want 1", stats.Subscriptions)
	}
}

