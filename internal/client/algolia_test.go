package client

import (
	"net/url"
	"testing"
)

func TestAlgoliaParamsFacetFilters(t *testing.T) {
	t.Run("with filters", func(t *testing.T) {
		p := algoliaParams("gambas", 3, []string{"categories.id:-17", "categories.id:-14"})
		vals, err := url.ParseQuery(p)
		if err != nil {
			t.Fatal(err)
		}
		if vals.Get("query") != "gambas" {
			t.Fatalf("query=%q", vals.Get("query"))
		}
		if vals.Get("hitsPerPage") != "3" {
			t.Fatalf("hitsPerPage=%q", vals.Get("hitsPerPage"))
		}
		// Flat JSON array → Algolia ANDs the entries.
		if got, want := vals.Get("facetFilters"), `["categories.id:-17","categories.id:-14"]`; got != want {
			t.Fatalf("facetFilters=%q want %q", got, want)
		}
	})
	t.Run("no filters omits the param", func(t *testing.T) {
		p := algoliaParams("x", 1, nil)
		vals, err := url.ParseQuery(p)
		if err != nil {
			t.Fatal(err)
		}
		if vals.Has("facetFilters") {
			t.Fatalf("facetFilters should be absent, got %q", vals.Get("facetFilters"))
		}
	})
}
