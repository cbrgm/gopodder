package gopodder

import "testing"

func TestIsValidUsername(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"alice", true},
		{"bob123", true},
		{"my.user", true},
		{"my-user", true},
		{"my_user", true},
		{"A", true},
		{"user.name-with_all.123", true},
		{"a23456789012345678901234567890123456789012345678901234567890abcd", true},

		{"", false},
		{"a234567890123456789012345678901234567890123456789012345678901abcde", false},
		{"has space", false},
		{"has/slash", false},
		{"../traversal", false},
		{"user@name", false},
		{"<script>", false},
		{"user\nname", false},
		{"user\x00name", false},
		{"日本語", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidUsername(tt.input)
			if got != tt.want {
				t.Errorf("isValidUsername(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidFeedURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://example.com/feed.xml", true},
		{"http://example.com/feed.xml", true},
		{"https://feeds.megaphone.fm/hubermanlab", true},
		{"http://downloads.bbc.co.uk/podcasts/radio4/inscience/rss.xml", true},
		{"https://example.com/feed?format=rss&id=123", true},

		{"", false},
		{"javascript:alert(1)", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"file:///etc/passwd", false},
		{"ftp://example.com/feed.xml", false},
		{"feed://example.com/feed.xml", false},
		{"not a url at all", false},
		{"://missing-scheme.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidFeedURL(tt.input)
			if got != tt.want {
				t.Errorf("isValidFeedURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFilterValidURLs(t *testing.T) {
	input := []string{
		"https://good.com/feed",
		"javascript:alert(1)",
		"http://also-good.com/rss",
		"file:///etc/passwd",
		"",
		"https://another.com/pod.xml",
	}

	got := filterValidURLs(input)
	if len(got) != 3 {
		t.Fatalf("expected 3 valid URLs, got %d: %v", len(got), got)
	}
	if got[0] != "https://good.com/feed" {
		t.Errorf("got[0] = %q", got[0])
	}
	if got[1] != "http://also-good.com/rss" {
		t.Errorf("got[1] = %q", got[1])
	}
	if got[2] != "https://another.com/pod.xml" {
		t.Errorf("got[2] = %q", got[2])
	}
}

func TestFilterValidURLs_Empty(t *testing.T) {
	got := filterValidURLs(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}

	got = filterValidURLs([]string{})
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestFilterValidURLs_AllInvalid(t *testing.T) {
	input := []string{"javascript:x", "data:text/html,x", "ftp://x"}
	got := filterValidURLs(input)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestFilterValidURLs_AllValid(t *testing.T) {
	input := []string{"http://a.com", "https://b.com", "http://c.com/feed.xml"}
	got := filterValidURLs(input)
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}
