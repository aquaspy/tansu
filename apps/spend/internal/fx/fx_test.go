package fx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aquasp/kuraspend/internal/store"
)

// Fixtures keep the real converter-box markup (attribute order included),
// trimmed to the quote div. Live values verified 2026-09-25: 5,19 / 5,91.
const (
	dollarFixture = `<div id="cotacao"><span class="cotMoeda estrangeira"><span class="symbol">US$</span><input type="text" id="estrangeiro" value="1,00"/></span><span class="optional"> vale </span><span class="cotMoeda nacional"><span class="symbol">R$</span><input type="text" id="nacional" value="5,19"/></span><span class="optional"> hoje</span><br class="clearfix"/></div>`
	euroFixture   = `<div id="cotacao"><span class="cotMoeda estrangeira"><span class="symbol">€</span><input type="text" id="estrangeiro" value="1,00"/></span><span class="optional"> vale </span><span class="cotMoeda nacional"><span class="symbol">R$</span><input type="text" id="nacional" value="5,91"/></span><span class="optional"> hoje</span><br class="clearfix"/></div>`
)

func TestParseRate(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		want  string
		valid bool
	}{
		{"dollar", dollarFixture, "5.19", true},
		{"euro", euroFixture, "5.91", true},
		{"reversed attrs", `<input value="6,02" type="text" id="nacional"/>`, "6.02", true},
		{"thousands", `<input id="nacional" value="1.234,56"/>`, "1234.56", true},
		{"dot decimal", `<input id="nacional" value="5.19"/>`, "5.19", true},
		{"missing box", `<div id="cotacao"></div>`, "", false},
		{"empty page", ``, "", false},
		{"garbage value", `<input id="nacional" value="abc"/>`, "", false},
		{"zero", `<input id="nacional" value="0,00"/>`, "", false},
		{"negative", `<input id="nacional" value="-5,19"/>`, "", false},
		{"absurd", `<input id="nacional" value="99999999"/>`, "", false},
	}
	for _, c := range cases {
		got, err := ParseRate([]byte(c.body))
		if c.valid {
			if err != nil || got != c.want {
				t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
			}
		} else if err == nil {
			t.Errorf("%s: got %q, want error", c.name, got)
		}
	}
}

func quoteServer(t *testing.T, dollar, euro string, euroStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/euro-hoje/" {
			w.WriteHeader(euroStatus)
			_, _ = w.Write([]byte(euro))
			return
		}
		_, _ = w.Write([]byte(dollar))
	}))
}

func TestRefreshCachesBoth(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	srv := quoteServer(t, dollarFixture, euroFixture, http.StatusOK)
	defer srv.Close()

	n, err := RefreshTarget(context.Background(), st, srv.Client(),
		[2]target{{"USD", srv.URL}, {"EUR", srv.URL + "/euro-hoje/"}})
	if err != nil || n != 2 {
		t.Fatalf("refresh = %d, %v", n, err)
	}
	quotes := Quotes(st)
	if quotes["USD"].Rate != "5.19" || quotes["EUR"].Rate != "5.91" {
		t.Fatalf("cached: %+v", quotes)
	}
	if !quotes["USD"].Fresh || time.Since(quotes["USD"].FetchedAt) > time.Minute {
		t.Fatalf("freshness: %+v", quotes["USD"])
	}
}

func TestRefreshPartialFailure(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	srv := quoteServer(t, dollarFixture, "boom", http.StatusInternalServerError)
	defer srv.Close()

	n, err := RefreshTarget(context.Background(), st, srv.Client(),
		[2]target{{"USD", srv.URL}, {"EUR", srv.URL + "/euro-hoje/"}})
	if err != nil || n != 1 {
		t.Fatalf("partial refresh = %d, %v", n, err)
	}
	if got := Quotes(st); got["USD"].Rate != "5.19" || got["EUR"].Rate != "" {
		t.Fatalf("cached: %+v", got)
	}
}

func TestRefreshTotalFailure(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	srv := quoteServer(t, "no box here", "no box here", http.StatusOK)
	defer srv.Close()

	n, err := RefreshTarget(context.Background(), st, srv.Client(),
		[2]target{{"USD", srv.URL}, {"EUR", srv.URL + "/euro-hoje/"}})
	if err == nil || n != 0 {
		t.Fatalf("failed refresh = %d, %v", n, err)
	}
}

func TestEffectiveRates(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	digest := []byte("x")
	u, _ := st.CreateUser("you@x.com", string(digest))
	if _, _, err := st.UpdateSettings(u.ID, "BRL", "", "BRL",
		map[string]string{"USD": "9.99"}); err != nil {
		t.Fatal(err)
	}
	u, _ = st.FindUser(u.ID)

	if err := st.UpsertFXRate("USD", "5.19"); err != nil {
		t.Fatal(err)
	}
	got := EffectiveRates(st, u)
	if got["USD"] != "5.19" {
		t.Fatalf("fresh auto should win: %v", got)
	}

	if _, err := st.DB().Exec(`UPDATE fx_rates SET fetched_at = '2020-01-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}
	got = EffectiveRates(st, u)
	if got["USD"] != "9.99" {
		t.Fatalf("stale auto should fall back to manual: %v", got)
	}

	u.HomeCurrency = "USD"
	if err := st.UpsertFXRate("EUR", "5.91"); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveRates(st, u); got["EUR"] != "" {
		t.Fatalf("auto must not apply when home is not BRL: %v", got)
	}
}
