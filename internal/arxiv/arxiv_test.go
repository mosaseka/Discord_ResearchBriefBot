package arxiv

import (
	"os"
	"testing"
)

func TestParseFixture(t *testing.T) {
	body, err := os.ReadFile("../../testdata/arxiv.xml")
	if err != nil {
		t.Fatal(err)
	}
	items, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d", len(items))
	}
	if items[0].ArXivID != "2609.00001" || items[0].DOI != "10.5555/jitai.2026.1" {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
}
func TestParseMalformed(t *testing.T) {
	if _, err := Parse([]byte("<feed><entry>")); err == nil {
		t.Fatal("expected malformed XML error")
	}
}
