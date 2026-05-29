package gopodder

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
)

const (
	isoFormat        = "2006-01-02T15:04:05"
	episodeBatchSize = 500
)

func toEpisode(device sql.NullString, podcast, episode string, ts sql.NullInt64, guid sql.NullString, action string, started, position, total sql.NullInt64) Episode {
	return Episode{
		Podcast:   podcast,
		Episode:   episode,
		Device:    nullStringPtr(device),
		Timestamp: nullInt64ToISO(ts),
		GUID:      nullStringPtr(guid),
		Action:    action,
		Started:   nullInt64Ptr(started),
		Position:  nullInt64Ptr(position),
		Total:     nullInt64Ptr(total),
	}
}

func episodeHash(ep Episode) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "a=%s;", ep.Action)
	if ep.Started != nil {
		_, _ = fmt.Fprintf(h, "s=%d;", *ep.Started)
	}
	if ep.Position != nil {
		_, _ = fmt.Fprintf(h, "p=%d;", *ep.Position)
	}
	if ep.Total != nil {
		_, _ = fmt.Fprintf(h, "t=%d;", *ep.Total)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func nullStringPtr(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}

func nullStringVal(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func nullInt64Ptr(ni sql.NullInt64) *int64 {
	if ni.Valid {
		return &ni.Int64
	}
	return nil
}

func nullInt64ToTime(ni sql.NullInt64) *time.Time {
	if !ni.Valid {
		return nil
	}
	return new(time.Unix(ni.Int64, 0))
}

func nullInt64ToISO(ni sql.NullInt64) *string {
	if !ni.Valid {
		return nil
	}
	return new(time.Unix(ni.Int64, 0).UTC().Format(isoFormat))
}

func ptrToNullString(s *string) sql.NullString {
	if s != nil {
		return sql.NullString{String: *s, Valid: true}
	}
	return sql.NullString{}
}

func ptrToNullInt64(i *int64) sql.NullInt64 {
	if i != nil {
		return sql.NullInt64{Int64: *i, Valid: true}
	}
	return sql.NullInt64{}
}

func ptrStringOr(s *string, def string) string {
	if s != nil {
		return *s
	}
	return def
}

func isoTimestampToNullInt64(s *string) sql.NullInt64 {
	if s == nil || *s == "" {
		return sql.NullInt64{}
	}
	t, err := time.Parse(isoFormat, *s)
	if err != nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	default:
		return 0
	}
}

func diffSubscriptions(existing, desired []string) (add, remove []string) {
	existingSet := make(map[string]struct{}, len(existing))
	for _, u := range existing {
		existingSet[u] = struct{}{}
	}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, u := range desired {
		desiredSet[u] = struct{}{}
	}

	for _, u := range desired {
		if _, ok := existingSet[u]; !ok {
			add = append(add, u)
		}
	}
	for _, u := range existing {
		if _, ok := desiredSet[u]; !ok {
			remove = append(remove, u)
		}
	}
	return add, remove
}
