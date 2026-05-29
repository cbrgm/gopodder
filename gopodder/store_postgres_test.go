package gopodder

import (
	"testing"
)

func TestNewPostgresStore_InvalidDSN(t *testing.T) {
	_, err := NewPostgresStore("postgres://invalid:invalid@localhost:1/nonexistent?connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for unreachable postgres")
	}
}

func TestNewPostgresStore_EmptyDSN(t *testing.T) {
	_, err := NewPostgresStore("")
	if err == nil {
		t.Fatal("expected error for empty DSN")
	}
}

func TestNewPostgresStore_MalformedDSN(t *testing.T) {
	_, err := NewPostgresStore("not-a-valid-url-at-all://???")
	if err == nil {
		t.Fatal("expected error for malformed DSN")
	}
}
