package config

import (
	"strings"
	"testing"
)

func TestDecodeFeeds(t *testing.T) {
	feeds, err := DecodeFeeds(strings.NewReader("feeds:\n  - name: Research\n    url: https://example.org/feed.xml\n    enabled: true\n    weight: 3\n    language: ja\n    category: institute\n    type: technical_report\n"))
	if err != nil || len(feeds) != 1 || !feeds[0].Enabled {
		t.Fatalf("feeds=%v err=%v", feeds, err)
	}
}
func TestDecodeFeedsRejectsHTTP(t *testing.T) {
	_, err := DecodeFeeds(strings.NewReader("feeds:\n  - name: x\n    url: http://127.0.0.1/x\n    enabled: true\n    weight: 1\n    language: en\n    category: x\n    type: research_feed\n"))
	if err == nil {
		t.Fatal("expected HTTP rejection")
	}
}

func TestDecodeFeedsRejectsDuplicate(t *testing.T) {
	_, err := DecodeFeeds(strings.NewReader("feeds:\n  - name: Research\n    url: https://example.org/a.xml\n    enabled: true\n    weight: 1\n    language: en\n    category: x\n    type: research_feed\n  - name: Research\n    url: https://example.org/b.xml\n    enabled: false\n    weight: 1\n    language: en\n    category: x\n    type: research_feed\n"))
	if err == nil {
		t.Fatal("expected duplicate rejection")
	}
}
