package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const catalogFixture = `{"data":[
	{"id":"vis/model","architecture":{"modality":"text+image->text"},"pricing":{"prompt":"0.0000002","completion":"0.0000004"}},
	{"id":"txt/model","architecture":{"modality":"text->text"},"pricing":{"prompt":"0.000003","completion":"0.000009"}},
	{"id":"free/model","architecture":{"modality":"text->text"},"pricing":{"prompt":"0","completion":"0"}},
	{"id":"noprice/model","architecture":{"modality":"text->text"},"pricing":{"prompt":"abc","completion":"0"}},
	{"id":"weird/model","architecture":{}}
]}`

func catalogServer(t *testing.T, hits *atomic.Int32, code int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if r.URL.Path != "/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(code)
		w.Write([]byte(catalogFixture))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCatalogSupportsImage(t *testing.T) {
	var hits atomic.Int32
	c := NewCatalog(catalogServer(t, &hits, 200).URL, "")
	ctx := context.Background()
	if !c.SupportsImage(ctx, "vis/model") {
		t.Fatal("vis/model = false")
	}
	if c.SupportsImage(ctx, "txt/model") {
		t.Fatal("txt/model = true")
	}
	if !c.SupportsImage(ctx, "weird/model") {
		t.Fatal("missing modality should fail open")
	}
	if !c.SupportsImage(ctx, "nope/model") {
		t.Fatal("unknown model should fail open")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1 (cached)", n)
	}
}

func TestCatalogFailOpen(t *testing.T) {
	c := NewCatalog(catalogServer(t, nil, 500).URL, "")
	if !c.SupportsImage(context.Background(), "txt/model") {
		t.Fatal("error should fail open")
	}
	var nilCatalog *Catalog
	if !nilCatalog.SupportsImage(context.Background(), "txt/model") {
		t.Fatal("nil catalog should fail open without fetching")
	}
}

func TestCatalogBlendedPrice(t *testing.T) {
	var hits atomic.Int32
	c := NewCatalog(catalogServer(t, &hits, 200).URL, "")
	ctx := context.Background()
	closeEnough := func(p, want float64) bool {
		d := p - want
		return d < 1e-9 && d > -1e-9
	}
	// (0.2e-6 + 0.4e-6) / 2 * 1e6 = $0.30/1M
	if p, ok := c.BlendedPrice(ctx, "vis/model"); !ok || !closeEnough(p, 0.3) {
		t.Fatalf("vis/model = (%v, %v), want (0.3, true)", p, ok)
	}
	// (3e-6 + 9e-6) / 2 * 1e6 = $6/1M
	if p, ok := c.BlendedPrice(ctx, "txt/model"); !ok || !closeEnough(p, 6) {
		t.Fatalf("txt/model = (%v, %v), want (6, true)", p, ok)
	}
	if p, ok := c.BlendedPrice(ctx, "free/model"); !ok || p != 0 {
		t.Fatalf("free/model = (%v, %v), want (0, true)", p, ok)
	}
	if _, ok := c.BlendedPrice(ctx, "noprice/model"); ok {
		t.Fatal("unparseable pricing should report ok=false")
	}
	if _, ok := c.BlendedPrice(ctx, "nope/model"); ok {
		t.Fatal("unknown model should report ok=false")
	}
	if _, ok := (*Catalog)(nil).BlendedPrice(ctx, "vis/model"); ok {
		t.Fatal("nil catalog should report ok=false without fetching")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1 (shared with vision cache)", n)
	}
}

func TestPriceTier(t *testing.T) {
	cases := []struct {
		blended float64
		want    string
	}{
		{0, TierCheap},
		{0.30, TierCheap},
		{1, TierCheap}, // boundary is cheap
		{2.25, TierMedium},
		{6, TierMedium},
		{10, TierExpensive}, // boundary is expensive
		{12, TierExpensive},
	}
	for _, tc := range cases {
		if got := PriceTier(tc.blended, 1, 10); got != tc.want {
			t.Errorf("PriceTier(%v) = %q, want %q", tc.blended, got, tc.want)
		}
	}
}
