package views

import (
	"testing"

	"github.com/aquasp/kuraaccount/internal/store"
)

func TestChestMatchesRegisteredApps(t *testing.T) {
	clients := make([]*store.Client, 6)
	linked := map[string]bool{}
	for i := range clients {
		id := "app" + string(rune('a'+i))
		clients[i] = &store.Client{ID: id, Name: id}
		if i < 4 {
			linked[id] = true
		}
	}
	d := HubData{Clients: clients, Linked: linked}
	if AppCount(d) != 6 || LinkedCount(d) != 4 {
		t.Fatalf("count = %d/%d", LinkedCount(d), AppCount(d))
	}
	chest := Chest(d)
	if len(chest.Drawers) != 6 {
		t.Fatalf("drawers = %d", len(chest.Drawers))
	}
	if got := rowCounts(6); len(got) != 4 || got[0] != 2 || got[1] != 1 || got[2] != 2 || got[3] != 1 {
		t.Fatalf("rows = %v", got)
	}
	lit := 0
	for i, drawer := range chest.Drawers {
		if drawer.Lit {
			lit++
		}
		if drawer.Lock != (i == len(chest.Drawers)-1) {
			t.Fatalf("drawer %d lock = %v", i, drawer.Lock)
		}
		if drawer.Y+drawer.H > chest.Case.Y+chest.Case.H {
			t.Fatalf("drawer %d escapes the case", i)
		}
	}
	if lit != 4 || !chest.Drawers[5].Lit || chest.Drawers[0].Lit {
		t.Fatalf("lighting = %+v", chest.Drawers)
	}
}

func TestChestScalesWithTheSuite(t *testing.T) {
	for n := 0; n <= 12; n++ {
		clients := make([]*store.Client, n)
		for i := range clients {
			clients[i] = &store.Client{ID: "c" + string(rune('a'+i))}
		}
		chest := Chest(HubData{Clients: clients})
		if len(chest.Drawers) != n {
			t.Fatalf("n=%d drawers=%d", n, len(chest.Drawers))
		}
		locks := 0
		for _, drawer := range chest.Drawers {
			if drawer.Lock {
				locks++
			}
			if drawer.W < 8 || drawer.H < 8 {
				t.Fatalf("n=%d tiny drawer %+v", n, drawer)
			}
		}
		if n > 0 && locks != 1 {
			t.Fatalf("n=%d locks=%d", n, locks)
		}
	}
}
