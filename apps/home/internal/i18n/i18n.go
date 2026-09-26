// Package i18n holds the en/pt string tables ported from
// config/locales. Keys use the same dot paths as the Rails app,
// with activerecord names flattened under attr.* and err.*.
package i18n

import (
	"strings"
)

type Locale string

const (
	EN Locale = "en"
	PT Locale = "pt"
)

// FromHeader mirrors the Rails Locale concern: first pt/en tag wins,
// default en.
func FromHeader(header string) Locale {
	for _, part := range strings.Split(header, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.Split(part, ";")[0]))
		if strings.HasPrefix(tag, "pt") {
			return PT
		}
		if strings.HasPrefix(tag, "en") {
			return EN
		}
	}
	return EN
}

func HTMLLang(l Locale) string {
	if l == PT {
		return "pt-BR"
	}
	return "en"
}

// T looks up key ("app.offline") and interpolates %{name} pairs.
func T(l Locale, key string, pairs ...string) string {
	s := lookup(l, key)
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "%{"+pairs[i]+"}", pairs[i+1])
	}
	return s
}

func lookup(l Locale, key string) string {
	if s, ok := strings_[l][key]; ok {
		return s
	}
	if s, ok := strings_[EN][key]; ok {
		return s
	}
	return key
}

// JS returns the js.* table serialized into the #i18n blob for scripts.
func JS(l Locale) map[string]string {
	out := map[string]string{}
	for k, v := range strings_[EN] {
		if name, ok := strings.CutPrefix(k, "js."); ok {
			out[name] = v
		}
	}
	if l != EN {
		for k, v := range strings_[l] {
			if name, ok := strings.CutPrefix(k, "js."); ok {
				out[name] = v
			}
		}
	}
	return out
}

// Sentence joins messages like Rails to_sentence ("a, b, and c").
func Sentence(l Locale, msgs []string) string {
	switch len(msgs) {
	case 0:
		return ""
	case 1:
		return msgs[0]
	case 2:
		if l == PT {
			return msgs[0] + " e " + msgs[1]
		}
		return msgs[0] + " and " + msgs[1]
	default:
		sep := ", "
		last := ", and "
		if l == PT {
			last = ", e "
		}
		return strings.Join(msgs[:len(msgs)-1], sep) + last + msgs[len(msgs)-1]
	}
}

var strings_ = map[Locale]map[string]string{
	EN: {
		"pwa.description": "A quiet homepage for the sites you actually open.",

		"titles.app":      "TansuHome",
		"titles.login":    "Sign in · TansuHome",
		"titles.signup":   "Create account · TansuHome",
		"titles.password": "Change password · TansuHome",
		"titles.lock":     "Unlock · TansuHome",
		"titles.stack":    "My stack · TansuHome",

		"auth.sign_in":              "Sign in",
		"auth.sign_in_lede":         "Your home lives on this server. Sign in to open it.",
		"auth.email":                "Email",
		"auth.password":             "Password",
		"auth.confirm_password":     "Confirm password",
		"auth.current_password":     "Current password",
		"auth.new_password":         "New password",
		"auth.confirm_new_password": "Confirm new password",
		"auth.change_password":      "Change password",
		"auth.change_password_lede": "The new password is used to sign in and to unlock the app.",
		"auth.password_changed":     "Password changed.",
		"auth.no_account":           "No account?",
		"auth.create_one":           "Create one",
		"auth.create_account":       "Create account",
		"auth.signup_lede":          "There is no password recovery. If you lose it, ask whoever runs the server.",
		"auth.have_account":         "Already have an account?",
		"auth.theme":                "Theme",
		"auth.too_many":             "Too many attempts. Wait a moment.",
		"auth.or":                   "or",
		"auth.login_with_kura":      "Sign in with Tansu",
		"auth.kura_failed":          "Tansu sign-in failed. Try again.",
		"auth.kura_signup_closed":   "No Tansu account is linked to this email, and new accounts are turned off.",
		"auth.signup_closed":        "New accounts are turned off on this server.",

		"app.unlock":                     "Unlock",
		"app.unlock_lede":                "Signed in as %{email}. Enter your password to show your home.",
		"app.lock":                       "Lock",
		"app.auto_lock_on":               "Turn on auto lock",
		"app.auto_lock_off":              "Turn off auto lock",
		"app.sign_out":                   "Sign out",
		"app.cancel":                     "Cancel",
		"app.delete":                     "Delete",
		"app.save":                       "Save",
		"app.more":                       "More",
		"app.offline":                    "You're offline. You can open this home, but changes won't save.",
		"app.default_profile":            "Personal",
		"app.last_profile":               "Keep at least one profile.",
		"app.add_site":                   "Add a site",
		"app.add_profile":                "Add a profile",
		"app.edit_site":                  "Edit site",
		"app.edit_profile":               "Rename profile",
		"app.empty_title":                "Nothing here yet",
		"app.empty_lede":                 "Add the sites you actually open. College, work, the two or three you hit every day.",
		"app.title":                      "Name",
		"app.url":                        "Address",
		"app.hint":                       "Note",
		"app.hint_placeholder":           "Optional. Shown under the name.",
		"app.url_placeholder":            "faculdade.edu.br",
		"app.profile_name":               "Profile name",
		"app.profile_placeholder":        "Work, personal, college…",
		"app.views":                      "Views",
		"app.sites_view":                 "Sites",
		"app.stack_view":                 "Stack",
		"app.stack_title":                "My stack",
		"app.stack_lede":                 "What you actually use. Not a wishlist.",
		"app.stack_empty_title":          "No stack yet",
		"app.stack_empty_lede":           "Music, browser, VPN, the registrar. One line each: what it is, where it comes from, and why it stayed.",
		"app.add_stack_item":             "Add a line",
		"app.edit_stack_item":            "Edit line",
		"app.stack_category":             "Category",
		"app.stack_choice":               "Choice",
		"app.stack_origin":               "Origin",
		"app.stack_note":                 "Note",
		"app.stack_category_placeholder": "Music, browser, email…",
		"app.stack_choice_placeholder":   "Tidal, Brave, Mailcow…",
		"app.stack_origin_placeholder":   "USA / Norway",
		"app.stack_note_placeholder":     "Why this one.",
		"app.stack_url_placeholder":      "Optional link",
		"app.icon":                       "Icon",
		"app.icon_placeholder":           "Optional. Blank uses the site's favicon.",
		"app.share_stack":                "Share image",

		"js.invalid_credentials":    "Invalid email or password.",
		"js.wrong_password":         "Wrong password.",
		"js.theme_system":           "Theme: system",
		"js.theme_light":            "Theme: light",
		"js.theme_dark":             "Theme: dark",
		"js.theme_switch":           "click to switch",
		"js.morning":                "Good morning",
		"js.afternoon":              "Good afternoon",
		"js.evening":                "Good evening",
		"js.search_ddg":             "DuckDuckGo",
		"js.search_startpage":       "Startpage",
		"js.search_kagi":            "Kagi",
		"js.search_google":          "Google",
		"js.search_brave":           "Brave",
		"js.search_placeholder":     "Search the web",
		"js.edit":                   "Edit",
		"js.done":                   "Done",
		"js.delete_site_confirm":    "Remove this site?",
		"js.delete_profile_confirm": "Delete this profile and every site in it?",
		"js.delete_stack_confirm":   "Remove this line?",
		"js.add_profile":            "Add a profile",

		"attr.user.email":                 "Email",
		"attr.user.password":              "Password",
		"attr.user.password_confirmation": "Password confirmation",
		"attr.profile.name":               "Name",
		"attr.site.title":                 "Name",
		"attr.site.url":                   "Address",
		"attr.site.hint":                  "Note",
		"attr.site.icon_url":              "Icon",
		"attr.stack.category":             "Category",
		"attr.stack.choice":               "Choice",
		"attr.stack.origin":               "Origin",
		"attr.stack.note":                 "Note",
		"attr.stack.url":                  "Address",
		"attr.stack.icon_url":             "Icon",

		"err.blank":    "can't be blank",
		"err.invalid":  "is invalid",
		"err.taken":    "has already been taken",
		"err.too_long": "is too long (maximum is %{count} characters)",

		"err.user.password.too_short":                 "is too short (minimum is %{count} characters)",
		"err.user.password_confirmation.confirmation": "doesn't match password",
		"err.profile.name.taken":                      "is already used",
		"err.profile.base.too_many":                   "Twelve profiles is enough.",
		"err.site.url.invalid":                        "needs to be a web address",
		"err.site.icon_url.invalid":                   "needs to be a web address",
		"err.site.base.too_many":                      "Sixty sites on this profile is enough.",
		"err.stack.url.invalid":                       "needs to be a web address",
		"err.stack.icon_url.invalid":                  "needs to be a web address",
		"err.stack.base.too_many":                     "Eighty lines on this stack is enough.",
	},
	PT: {
		"pwa.description": "Uma homepage calma para os sites que você realmente abre.",

		"titles.app":      "TansuHome",
		"titles.login":    "Entrar · TansuHome",
		"titles.signup":   "Criar conta · TansuHome",
		"titles.password": "Mudar senha · TansuHome",
		"titles.lock":     "Desbloquear · TansuHome",
		"titles.stack":    "Meu stack · TansuHome",

		"auth.sign_in":              "Entrar",
		"auth.sign_in_lede":         "Sua home fica neste servidor. Entre para abri-la.",
		"auth.email":                "Email",
		"auth.password":             "Senha",
		"auth.confirm_password":     "Confirmar senha",
		"auth.current_password":     "Senha atual",
		"auth.new_password":         "Nova senha",
		"auth.confirm_new_password": "Confirmar nova senha",
		"auth.change_password":      "Mudar senha",
		"auth.change_password_lede": "A senha nova vale para entrar e para desbloquear o app.",
		"auth.password_changed":     "Senha alterada.",
		"auth.no_account":           "Sem conta?",
		"auth.create_one":           "Criar uma",
		"auth.create_account":       "Criar conta",
		"auth.signup_lede":          "Não existe recuperação de senha. Se perder, peça a quem administra o servidor.",
		"auth.have_account":         "Já tem conta?",
		"auth.theme":                "Tema",
		"auth.too_many":             "Muitas tentativas. Espere um pouco.",
		"auth.or":                   "ou",
		"auth.login_with_kura":      "Entrar com Tansu",
		"auth.kura_failed":          "O login com Tansu falhou. Tente de novo.",
		"auth.kura_signup_closed":   "Nenhuma conta Tansu vinculada a este email, e novos cadastros estão desligados.",
		"auth.signup_closed":        "Novas contas estão desligadas neste servidor.",

		"app.unlock":                     "Desbloquear",
		"app.unlock_lede":                "Conectado como %{email}. Digite a senha para ver sua home.",
		"app.lock":                       "Bloquear",
		"app.auto_lock_on":               "Ligar bloqueio automático",
		"app.auto_lock_off":              "Desligar bloqueio automático",
		"app.sign_out":                   "Sair",
		"app.cancel":                     "Cancelar",
		"app.delete":                     "Apagar",
		"app.save":                       "Salvar",
		"app.more":                       "Mais",
		"app.offline":                    "Você está offline. Pode ver esta home, mas não salvar.",
		"app.default_profile":            "Pessoal",
		"app.last_profile":               "Deixe pelo menos um perfil.",
		"app.add_site":                   "Adicionar site",
		"app.add_profile":                "Adicionar perfil",
		"app.edit_site":                  "Editar site",
		"app.edit_profile":               "Renomear perfil",
		"app.empty_title":                "Nada aqui ainda",
		"app.empty_lede":                 "Coloque os sites que você realmente abre. Faculdade, trabalho, os dois ou três de todo dia.",
		"app.title":                      "Nome",
		"app.url":                        "Endereço",
		"app.hint":                       "Nota",
		"app.hint_placeholder":           "Opcional. Aparece abaixo do nome.",
		"app.url_placeholder":            "faculdade.edu.br",
		"app.profile_name":               "Nome do perfil",
		"app.profile_placeholder":        "Trabalho, pessoal, faculdade…",
		"app.views":                      "Vistas",
		"app.sites_view":                 "Sites",
		"app.stack_view":                 "Stack",
		"app.stack_title":                "Meu stack",
		"app.stack_lede":                 "O que você realmente usa. Não é wishlist.",
		"app.stack_empty_title":          "Nenhum stack ainda",
		"app.stack_empty_lede":           "Música, browser, VPN, o registrar. Uma linha cada: o que é, de onde vem, e por que ficou.",
		"app.add_stack_item":             "Adicionar linha",
		"app.edit_stack_item":            "Editar linha",
		"app.stack_category":             "Categoria",
		"app.stack_choice":               "Escolha",
		"app.stack_origin":               "Origem",
		"app.stack_note":                 "Observação",
		"app.stack_category_placeholder": "Música, browser, email…",
		"app.stack_choice_placeholder":   "Tidal, Brave, Mailcow…",
		"app.stack_origin_placeholder":   "EUA / Noruega",
		"app.stack_note_placeholder":     "Por que este.",
		"app.stack_url_placeholder":      "Link opcional",
		"app.icon":                       "Ícone",
		"app.icon_placeholder":           "Opcional. Vazio usa o favicon do site.",
		"app.share_stack":                "Compartilhar imagem",

		"js.invalid_credentials":    "Email ou senha inválidos.",
		"js.wrong_password":         "Senha incorreta.",
		"js.theme_system":           "Tema: sistema",
		"js.theme_light":            "Tema: claro",
		"js.theme_dark":             "Tema: escuro",
		"js.theme_switch":           "clique para alternar",
		"js.morning":                "Bom dia",
		"js.afternoon":              "Boa tarde",
		"js.evening":                "Boa noite",
		"js.search_ddg":             "DuckDuckGo",
		"js.search_startpage":       "Startpage",
		"js.search_kagi":            "Kagi",
		"js.search_google":          "Google",
		"js.search_brave":           "Brave",
		"js.search_placeholder":     "Buscar na web",
		"js.edit":                   "Editar",
		"js.done":                   "Pronto",
		"js.delete_site_confirm":    "Remover este site?",
		"js.delete_profile_confirm": "Apagar este perfil e todos os sites dele?",
		"js.delete_stack_confirm":   "Remover esta linha?",
		"js.add_profile":            "Adicionar perfil",

		"attr.user.email":                 "Email",
		"attr.user.password":              "Senha",
		"attr.user.password_confirmation": "Confirmação da senha",
		"attr.profile.name":               "Nome",
		"attr.site.title":                 "Nome",
		"attr.site.url":                   "Endereço",
		"attr.site.hint":                  "Nota",
		"attr.site.icon_url":              "Ícone",
		"attr.stack.category":             "Categoria",
		"attr.stack.choice":               "Escolha",
		"attr.stack.origin":               "Origem",
		"attr.stack.note":                 "Observação",
		"attr.stack.url":                  "Endereço",
		"attr.stack.icon_url":             "Ícone",

		"err.blank":    "não pode ficar em branco",
		"err.invalid":  "não é válido",
		"err.taken":    "já está em uso",
		"err.too_long": "é muito longo (máximo: %{count} caracteres)",

		"err.user.password.too_short":                 "é muito curta (mínimo de %{count} caracteres)",
		"err.user.password_confirmation.confirmation": "não confere com a senha",
		"err.profile.name.taken":                      "já está em uso",
		"err.profile.base.too_many":                   "Doze perfis bastam.",
		"err.site.url.invalid":                        "precisa ser um endereço da web",
		"err.site.icon_url.invalid":                   "precisa ser um endereço da web",
		"err.site.base.too_many":                      "Sessenta sites neste perfil bastam.",
		"err.stack.url.invalid":                       "precisa ser um endereço da web",
		"err.stack.icon_url.invalid":                  "precisa ser um endereço da web",
		"err.stack.base.too_many":                     "Oitenta linhas neste stack bastam.",
	},
}
