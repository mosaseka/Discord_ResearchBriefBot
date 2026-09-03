package pubmed

import (
	"os"
	"strings"
	"testing"
)

func TestParseArticlesFixture(t *testing.T) {
	body, err := os.ReadFile("../../testdata/pubmed.xml")
	if err != nil {
		t.Fatal(err)
	}
	items, err := ParseArticles(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d", len(items))
	}
	first := items[0]
	if first.PMID != "40000001" || first.DOI != "10.5555/jitai.2026.1" || !first.PeerReviewed || !strings.Contains(first.Abstract, "N=240") {
		t.Fatalf("unexpected item: %+v", first)
	}
}
func TestParseSearch(t *testing.T) {
	ids, err := ParseSearch([]byte(`<eSearchResult><IdList><Id>1</Id><Id>2</Id></IdList></eSearchResult>`))
	if err != nil || len(ids) != 2 {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
}
func TestParseSearchAPIError(t *testing.T) {
	if _, err := ParseSearch([]byte(`<eSearchResult><ERROR>bad request</ERROR></eSearchResult>`)); err == nil {
		t.Fatal("expected API error")
	}
}
func TestParseMalformed(t *testing.T) {
	if _, err := ParseArticles([]byte("{")); err == nil {
		t.Fatal("expected malformed XML error")
	}
}
