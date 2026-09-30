package i18n

import "testing"

func TestEmptyListCopy(t *testing.T) {
	if got := T(EN, "chat.empty_list"); got != "Ask about your calendar, spend, or notes." {
		t.Fatalf("en: %q", got)
	}
	if got := T(PT, "chat.empty_list"); got != "Pergunte sobre a sua agenda, os seus gastos ou as suas notas." {
		t.Fatalf("pt: %q", got)
	}
}

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
