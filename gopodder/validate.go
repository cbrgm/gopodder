package gopodder

import (
	"net/url"
	"regexp"
)

var validIdentifier = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

func isValidUsername(s string) bool {
	return validIdentifier.MatchString(s)
}

func isValidFeedURL(s string) bool {
	if s == "" {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func filterValidURLs(urls []string) []string {
	valid := urls[:0]
	for _, u := range urls {
		if isValidFeedURL(u) {
			valid = append(valid, u)
		}
	}
	return valid
}
