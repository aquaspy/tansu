package i18n

import "testing"

func TestFromCookieDefaultLocale(t *testing.T) {
	t.Setenv("DEFAULT_LOCALE", "")
	if got := FromCookie("", ""); got != EN {
		t.Fatalf("unset: %s", got)
	}
	t.Setenv("DEFAULT_LOCALE", "pt")
	if got := FromCookie("", ""); got != PT {
		t.Fatalf("pt default: %s", got)
	}
	if got := FromCookie("en-US,en;q=0.9", ""); got != EN {
		t.Fatalf("accept-language wins: %s", got)
	}
	if got := FromCookie("pt", "en"); got != EN {
		t.Fatalf("cookie wins: %s", got)
	}
	t.Setenv("DEFAULT_LOCALE", "pt-BR")
	if got := FromCookie("fr", ""); got != PT {
		t.Fatalf("pt-br: %s", got)
	}
}
