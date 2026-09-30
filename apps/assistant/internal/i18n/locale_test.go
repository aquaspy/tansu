package i18n

import "testing"

func TestAnonymousBannerCopy(t *testing.T) {
	if got := T(EN, "mode.not_saved_banner"); got != "This chat will not be saved" {
		t.Fatalf("en banner: %q", got)
	}
	if got := T(PT, "mode.not_saved_banner"); got != "Esta conversa não será salva" {
		t.Fatalf("pt banner: %q", got)
	}
	if got := T(PT, "mode.anonymous_on"); got != "Anônimo ligado" {
		t.Fatalf("pt on: %q", got)
	}
	if got := T(EN, "mode.anonymous_on"); got != "Anonymous on" {
		t.Fatalf("en on: %q", got)
	}
	if got := T(PT, "mode.anon_empty"); got != "Pergunte qualquer coisa — nada fica no histórico." {
		t.Fatalf("pt empty: %q", got)
	}
}

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
