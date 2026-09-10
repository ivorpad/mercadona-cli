package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMyRegulars(t *testing.T) {
	const response = `[
	  {
	    "product": {
	      "id": "27348",
	      "display_name": "Refresco Coca-Cola",
	      "packaging": "Pack-2",
	      "published": true,
	      "price_instructions": {
	        "unit_price": "4.10",
	        "reference_price": "1.025",
	        "reference_format": "L"
	      }
	    },
	    "recommended_quantity": 3
	  },
	  {
	    "product": {
	      "id": "999",
	      "display_name": "Producto a peso",
	      "published": false,
	      "price_instructions": {"unit_price": "2.50"}
	    },
	    "recommended_quantity": 0.5
	  }
	]`

	var called int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/customers/customer-1/recommendations/myregulars/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("wh"); got != "vlc1" {
			t.Errorf("wh = %q, want vlc1", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer srv.Close()

	cl := New()
	cl.HTTP = srv.Client()
	cl.BaseURL = srv.URL
	cl.Token = "test-token"
	cl.CustomerID = "customer-1"
	cl.Warehouse = "vlc1"

	regulars, raw, err := cl.MyRegulars()
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("request count = %d, want 1", called)
	}
	if strings.TrimSpace(string(raw)) != strings.TrimSpace(response) {
		t.Fatalf("raw response changed:\n%s", raw)
	}
	if len(regulars) != 2 {
		t.Fatalf("len(regulars) = %d, want 2", len(regulars))
	}

	first := regulars[0]
	if first.Product.ID != "27348" || first.Product.DisplayName != "Refresco Coca-Cola" {
		t.Fatalf("first product identity = %+v", first.Product)
	}
	if first.Product.Packaging != "Pack-2" || !first.Product.Published {
		t.Fatalf("first product metadata = %+v", first.Product)
	}
	if first.Product.Price.UnitPrice != "4.10" || first.Product.Price.ReferencePrice != "1.025" || first.Product.Price.ReferenceFormat != "L" {
		t.Fatalf("first product price = %+v", first.Product.Price)
	}
	if first.RecommendedQuantity != 3 {
		t.Errorf("first recommended quantity = %v, want 3", first.RecommendedQuantity)
	}
	if regulars[1].RecommendedQuantity != 0.5 {
		t.Errorf("fractional recommended quantity = %v, want 0.5", regulars[1].RecommendedQuantity)
	}
	if regulars[1].Product.Published {
		t.Error("second product should be unpublished")
	}
}

func TestMyRegularsRejectsUnexpectedShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	cl := New()
	cl.HTTP = srv.Client()
	cl.BaseURL = srv.URL
	cl.Token = "test-token"
	cl.CustomerID = "customer-1"

	regulars, raw, err := cl.MyRegulars()
	if err == nil {
		t.Fatal("MyRegulars() error = nil, want a decode error")
	}
	if regulars != nil {
		t.Fatalf("regulars = %+v, want nil", regulars)
	}
	if string(raw) != `{"results":[]}` {
		t.Fatalf("raw = %s, want upstream response preserved", raw)
	}
}
