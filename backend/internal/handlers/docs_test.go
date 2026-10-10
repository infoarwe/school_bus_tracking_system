package handlers

import (
	"regexp"
	"strings"
	"testing"
)

// Every CDN file on /docs must be version-pinned and integrity-checked: the page
// shares its origin with the admin web's stored tokens.
func TestDocsPageCDNPinned(t *testing.T) {
	tags := regexp.MustCompile(`<(script|link)[^>]*https://[^>]*>`).FindAllString(swaggerUIPage, -1)
	if len(tags) != 2 {
		t.Fatalf("expected 2 CDN tags, found %d", len(tags))
	}
	for _, tag := range tags {
		if !strings.Contains(tag, `integrity="sha384-`) || !strings.Contains(tag, `crossorigin="anonymous"`) {
			t.Errorf("missing SRI: %s", tag)
		}
		if !regexp.MustCompile(`@\d+\.\d+\.\d+/`).MatchString(tag) {
			t.Errorf("version not pinned exactly: %s", tag)
		}
	}
}
