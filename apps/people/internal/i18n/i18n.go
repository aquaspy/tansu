// Package i18n holds the en/pt string tables. Keys use the same dot
// paths as the sibling apps.
package i18n

import (
	"os"
	"strings"
	"time"
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

// FromCookie prefers an explicit kura_locale cookie over Accept-Language.
func FromCookie(header, cookie string) Locale {
	switch strings.ToLower(strings.TrimSpace(cookie)) {
	case "pt", "pt-br":
		return PT
	case "en":
		return EN
	default:
		if loc, ok := localeFromHeader(header); ok {
			return loc
		}
		return DefaultLocale()
	}
}

// DefaultLocale applies when the locale cookie is empty and Accept-Language
// names neither Portuguese nor English. Hosted gettansu.com sets
// DEFAULT_LOCALE=pt. Unset or any other value stays English.
func DefaultLocale() Locale {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DEFAULT_LOCALE"))) {
	case "pt", "pt-br":
		return PT
	default:
		return EN
	}
}

func localeFromHeader(header string) (Locale, bool) {
	for _, part := range strings.Split(header, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.Split(part, ";")[0]))
		if tag == "" {
			continue
		}
		if strings.HasPrefix(tag, "pt") {
			return PT, true
		}
		if strings.HasPrefix(tag, "en") {
			return EN, true
		}
	}
	return "", false
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

// TimeShort mirrors the Rails time.formats.short: "Sep 19, 10:00" in en,
// "19/09, 10:00" in pt.
func TimeShort(l Locale, t time.Time) string {
	t = t.Local()
	if l == PT {
		return t.Format("02/01, 15:04")
	}
	return t.Format("Jan 2, 15:04")
}

// MonthNames mirrors date.month_names (January..December /
// janeiro..dezembro).
func MonthNames(l Locale) [12]string {
	if l == PT {
		return ptMonths
	}
	return [12]string{"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December"}
}

// MonthDayLabel formats a month/day pair ("August 11" / "11 de agosto").
func MonthDayLabel(l Locale, month, day int) string {
	names := MonthNames(l)
	if month < 1 || month > 12 {
		return ""
	}
	if l == PT {
		return itoa(day) + " de " + names[month-1]
	}
	return names[month-1] + " " + itoa(day)
}

var ptMonths = [12]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

var strings_ = map[Locale]map[string]string{
	EN: {
		"pwa.description": "The people in your life, as a PWA.",

		"titles.app":      "TansuPeople",
		"titles.login":    "Sign in · TansuPeople",
		"titles.signup":   "Create account · TansuPeople",
		"titles.password": "Change password · TansuPeople",
		"titles.lock":     "Unlock · TansuPeople",
		"titles.tokens":   "API tokens · TansuPeople",
		"titles.agent":    "Link Assistant · TansuPeople",

		"tokens.title":            "API tokens",
		"tokens.lede":             "Tokens let AI agents and other apps manage your people through the API. A token has full access to your cards, and keeps working while the app is locked. Treat it like a password.",
		"tokens.name_label":       "Name",
		"tokens.name_placeholder": "e.g. Hermes Agent",
		"tokens.create":           "Generate token",
		"tokens.copy_now":         "Copy this token now. It will not be shown again.",
		"tokens.existing":         "Active tokens",
		"tokens.empty":            "No tokens yet.",
		"tokens.last_used":        "Last used",
		"tokens.never_used":       "Never",
		"tokens.revoke":           "Revoke",
		"tokens.revoke_confirm":   "Revoke this token? Connected apps will stop working.",
		"tokens.revoked":          "Token revoked.",
		"tokens.name_blank":       "Name can't be blank.",
		"tokens.too_many":         "Ten tokens is enough. Revoke one first.",

		"agent.title":       "Link Tansu Assistant",
		"agent.lede":        "Tansu Assistant will read and change everything in this account, including deleting it. It can do that even while this app is locked. Text it reads is sent to the model, the same way a chat message is.",
		"agent.session":     "This session: %{email}",
		"agent.confirm":     "Allow",
		"agent.bad_request": "This link is not valid.",

		"auth.sign_in":              "Sign in",
		"auth.sign_in_lede":         "Your people live on this server. Sign in to open them.",
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
		"auth.language":             "Language",
		"auth.too_many":             "Too many attempts. Wait a moment.",
		"auth.or":                   "or",
		"auth.login_with_kura":      "Sign in with Tansu",
		"auth.kura_failed":          "Tansu sign-in failed. Try again.",
		"auth.kura_signup_closed":   "No Tansu account is linked to this email, and new accounts are turned off.",
		"auth.signup_closed":        "New accounts are turned off on this server.",
		"auth.email_invalid":        "That email doesn't look right.",
		"auth.email_taken":          "That email is already registered.",
		"auth.password_too_short":   "Password is too short (minimum 8 characters).",
		"auth.password_mismatch":    "Password confirmation doesn't match.",

		"app.unlock":                "Unlock",
		"app.unlock_lede":           "Signed in as %{email}. Enter your password to show your people.",
		"app.lock":                  "Lock",
		"app.auto_lock_on":          "Turn on auto lock",
		"app.auto_lock_off":         "Turn off auto lock",
		"app.sign_out":              "Sign out",
		"app.cancel":                "Cancel",
		"app.delete":                "Delete",
		"app.save":                  "Save",
		"app.more":                  "More",
		"app.account":               "Tansu Account",
		"app.short_name":            "People",
		"app.account_label":         "Open Tansu Account",
		"app.search":                "Search people, notes, relationships…",
		"app.all":                   "Everyone",
		"app.upcoming":              "Coming up",
		"app.today":                 "Today",
		"app.tomorrow":              "Tomorrow",
		"app.in_days":               "in %{count} days",
		"app.turns":                 "turns %{age}",
		"app.empty_title":           "Nobody here yet",
		"app.empty_lede":            "Add the people in your life — birthdays, sizes, gift ideas — so nothing slips.",
		"app.add_person":            "Add person",
		"app.new_person":            "New person",
		"app.edit_person":           "Edit person",
		"app.birthdays_export":      "Birthdays (.ics)",
		"app.calendar_export":       "TansuCalendar (.json)",
		"app.send_to_calendar":      "Send to calendar",
		"app.send_to_calendar_hint": "Downloads a birthday file to import under More → Import in TansuCalendar.",
		"app.call":                  "Call",
		"app.email_person":          "Email",
		"app.sizes":                 "Sizes",
		"app.height":                "Height",
		"app.ring_size":             "Ring",
		"app.shoe_size":             "Shoes",
		"app.shirt_size":            "Shirt",
		"app.pants_size":            "Pants",
		"app.favorites":             "Favorites",
		"app.favorites_hint":        "Colors, foods, little treats, hobbies — anything that helps a gift land.",
		"app.about":                 "About",
		"app.details":               "Details",
		"app.contact":               "Contact",
		"app.name":                  "Name",
		"app.nickname":              "Nickname",
		"app.nickname_hint":         "What you call them.",
		"app.relationship":          "Relationship",
		"app.relationship_hint":     "e.g. partner, mom, friend…",
		"app.birthday":              "Birthday",
		"app.month":                 "Month",
		"app.day":                   "Day",
		"app.year":                  "Year",
		"app.year_hint":             "Optional. Used to show age.",
		"app.emoji":                 "Emoji",
		"app.phone":                 "Phone",
		"app.email":                 "Email",
		"app.address":               "Address",
		"app.notes":                 "Notes",
		"app.custom":                "Anything else",
		"app.custom_hint":           "Ring engraving, coffee order, allergies — free rows for the specifics.",
		"app.attr_label":            "Label",
		"app.attr_value":            "Value",
		"app.add_field":             "Add field",
		"app.remove":                "Remove",
		"app.offline":               "You're offline. You can open cards you've already viewed, but changes won't save.",

		"errors.person.name.blank":            "Name can't be blank",
		"errors.person.name.too_long":         "Name is too long (200 characters max)",
		"errors.person.nickname.too_long":     "Nickname is too long (80 characters max)",
		"errors.person.relationship.too_long": "Relationship is too long (40 characters max)",
		"errors.person.emoji.too_long":        "Emoji is too long",
		"errors.person.phone.too_long":        "Phone is too long (40 characters max)",
		"errors.person.email.too_long":        "Email is too long (160 characters max)",
		"errors.person.address.too_long":      "Address is too long (500 characters max)",
		"errors.person.ring_size.too_long":    "Ring size is too long (20 characters max)",
		"errors.person.shoe_size.too_long":    "Shoe size is too long (20 characters max)",
		"errors.person.shirt_size.too_long":   "Shirt size is too long (20 characters max)",
		"errors.person.pants_size.too_long":   "Pants size is too long (20 characters max)",
		"errors.person.height.too_long":       "Height is too long (20 characters max)",
		"errors.person.favorites.too_long":    "Favorites are too long (2000 characters max)",
		"errors.person.notes.too_long":        "Notes are too long (8000 characters max)",
		"errors.person.birthday.range":        "Birthday must be a full month and day",
		"errors.person.birthday.invalid":      "Birthday isn't a real day in that month",
		"errors.person.birthday.needs_date":   "A birth year needs a month and day",
		"errors.person.birth_year.range":      "Year must be between 1900 and 2100",
		"errors.person.base.too_many":         "Five hundred people is enough.",

		"errors.attr.label.blank":    "Every extra row needs a label",
		"errors.attr.label.too_long": "An extra label is too long (80 characters max)",
		"errors.attr.value.too_long": "An extra value is too long (500 characters max)",
		"errors.attr.base.too_many":  "Fifty extra rows per person is enough.",

		"js.invalid_credentials":   "Invalid email or password.",
		"js.wrong_password":        "Wrong password.",
		"js.theme_system":          "Theme: system",
		"js.theme_light":           "Theme: light",
		"js.theme_dark":            "Theme: dark",
		"js.theme_switch":          "click to switch",
		"js.delete_person_confirm": "Delete %{name}? This cannot be undone.",
		"js.search_label":          "Search",
		"js.attr_label":            "Label",
		"js.attr_value":            "Value",
		"js.remove":                "Remove",
	},
	PT: {
		"pwa.description": "As pessoas da sua vida, como PWA.",

		"titles.app":      "TansuPeople",
		"titles.login":    "Entrar · TansuPeople",
		"titles.signup":   "Criar conta · TansuPeople",
		"titles.password": "Mudar senha · TansuPeople",
		"titles.lock":     "Desbloquear · TansuPeople",
		"titles.tokens":   "Tokens de API · TansuPeople",
		"titles.agent":    "Ligar o Assistant · TansuPeople",

		"tokens.title":            "Tokens de API",
		"tokens.lede":             "Tokens permitem que agentes de IA e outros apps gerenciem suas pessoas pela API. Um token tem acesso total aos cards e continua valendo com o app bloqueado. Trate como uma senha.",
		"tokens.name_label":       "Nome",
		"tokens.name_placeholder": "ex.: Hermes Agent",
		"tokens.create":           "Gerar token",
		"tokens.copy_now":         "Copie este token agora. Ele não será mostrado de novo.",
		"tokens.existing":         "Tokens ativos",
		"tokens.empty":            "Nenhum token ainda.",
		"tokens.last_used":        "Último uso",
		"tokens.never_used":       "Nunca",
		"tokens.revoke":           "Revogar",
		"tokens.revoke_confirm":   "Revogar este token? Os apps conectados vão parar de funcionar.",
		"tokens.revoked":          "Token revogado.",
		"tokens.name_blank":       "Nome não pode ficar em branco.",
		"tokens.too_many":         "Dez tokens bastam. Revogue um antes.",

		"agent.title":       "Ligar o Tansu Assistant",
		"agent.lede":        "O Tansu Assistant vai ler e alterar tudo nesta conta, inclusive apagar. Ele faz isso mesmo com este app trancado. O texto que ele ler segue para o modelo, como uma mensagem de chat.",
		"agent.session":     "Esta sessão: %{email}",
		"agent.confirm":     "Permitir",
		"agent.bad_request": "Este link não é válido.",

		"auth.sign_in":              "Entrar",
		"auth.sign_in_lede":         "Suas pessoas ficam neste servidor. Entre para abri-las.",
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
		"auth.language":             "Idioma",
		"auth.too_many":             "Muitas tentativas. Espere um pouco.",
		"auth.or":                   "ou",
		"auth.login_with_kura":      "Entrar com Tansu",
		"auth.kura_failed":          "O login com Tansu falhou. Tente de novo.",
		"auth.kura_signup_closed":   "Nenhuma conta Tansu vinculada a este email, e novos cadastros estão desligados.",
		"auth.signup_closed":        "Novas contas estão desligadas neste servidor.",
		"auth.email_invalid":        "Esse email não parece certo.",
		"auth.email_taken":          "Esse email já está registrado.",
		"auth.password_too_short":   "Senha muito curta (mínimo 8 caracteres).",
		"auth.password_mismatch":    "A confirmação não confere com a senha.",

		"app.unlock":                "Desbloquear",
		"app.unlock_lede":           "Conectado como %{email}. Digite a senha para ver suas pessoas.",
		"app.lock":                  "Bloquear",
		"app.auto_lock_on":          "Ligar bloqueio automático",
		"app.auto_lock_off":         "Desligar bloqueio automático",
		"app.sign_out":              "Sair",
		"app.cancel":                "Cancelar",
		"app.delete":                "Apagar",
		"app.save":                  "Salvar",
		"app.more":                  "Mais",
		"app.account":               "Tansu Account",
		"app.short_name":            "Pessoas",
		"app.account_label":         "Abrir o Tansu Account",
		"app.search":                "Buscar pessoas, notas, relações…",
		"app.all":                   "Todos",
		"app.upcoming":              "Chegando",
		"app.today":                 "Hoje",
		"app.tomorrow":              "Amanhã",
		"app.in_days":               "em %{count} dias",
		"app.turns":                 "faz %{age}",
		"app.empty_title":           "Ninguém por aqui ainda",
		"app.empty_lede":            "Adicione as pessoas da sua vida — aniversários, tamanhos, ideias de presente — para nada passar batido.",
		"app.add_person":            "Adicionar pessoa",
		"app.new_person":            "Nova pessoa",
		"app.edit_person":           "Editar pessoa",
		"app.birthdays_export":      "Aniversários (.ics)",
		"app.calendar_export":       "TansuCalendar (.json)",
		"app.send_to_calendar":      "Enviar para o calendar",
		"app.send_to_calendar_hint": "Baixa um arquivo de aniversário para importar em Mais → Importar no TansuCalendar.",
		"app.call":                  "Ligar",
		"app.email_person":          "Email",
		"app.sizes":                 "Tamanhos",
		"app.height":                "Altura",
		"app.ring_size":             "Anel",
		"app.shoe_size":             "Calçado",
		"app.shirt_size":            "Blusa",
		"app.pants_size":            "Calça",
		"app.favorites":             "Favoritos",
		"app.favorites_hint":        "Cores, comidas, mimos, hobbies — tudo que ajuda um presente a acertar.",
		"app.about":                 "Sobre",
		"app.details":               "Detalhes",
		"app.contact":               "Contato",
		"app.name":                  "Nome",
		"app.nickname":              "Apelido",
		"app.nickname_hint":         "Como você chama.",
		"app.relationship":          "Relação",
		"app.relationship_hint":     "ex.: namorada, mãe, amigo…",
		"app.birthday":              "Aniversário",
		"app.month":                 "Mês",
		"app.day":                   "Dia",
		"app.year":                  "Ano",
		"app.year_hint":             "Opcional. Serve para mostrar a idade.",
		"app.emoji":                 "Emoji",
		"app.phone":                 "Telefone",
		"app.email":                 "Email",
		"app.address":               "Endereço",
		"app.notes":                 "Notas",
		"app.custom":                "Qualquer outra coisa",
		"app.custom_hint":           "Aro da aliança, pedido do café, alergias — linhas livres para os detalhes.",
		"app.attr_label":            "Rótulo",
		"app.attr_value":            "Valor",
		"app.add_field":             "Adicionar campo",
		"app.remove":                "Remover",
		"app.offline":               "Você está offline. Pode abrir cards que já viu, mas não salvar.",

		"errors.person.name.blank":            "Nome não pode ficar em branco",
		"errors.person.name.too_long":         "Nome é muito longo (máximo 200 caracteres)",
		"errors.person.nickname.too_long":     "Apelido é muito longo (máximo 80 caracteres)",
		"errors.person.relationship.too_long": "Relação é muito longa (máximo 40 caracteres)",
		"errors.person.emoji.too_long":        "Emoji é muito longo",
		"errors.person.phone.too_long":        "Telefone é muito longo (máximo 40 caracteres)",
		"errors.person.email.too_long":        "Email é muito longo (máximo 160 caracteres)",
		"errors.person.address.too_long":      "Endereço é muito longo (máximo 500 caracteres)",
		"errors.person.ring_size.too_long":    "Tamanho do anel é muito longo (máximo 20 caracteres)",
		"errors.person.shoe_size.too_long":    "Tamanho do calçado é muito longo (máximo 20 caracteres)",
		"errors.person.shirt_size.too_long":   "Tamanho da blusa é muito longo (máximo 20 caracteres)",
		"errors.person.pants_size.too_long":   "Tamanho da calça é muito longo (máximo 20 caracteres)",
		"errors.person.height.too_long":       "Altura é muito longa (máximo 20 caracteres)",
		"errors.person.favorites.too_long":    "Favoritos são muito longos (máximo 2000 caracteres)",
		"errors.person.notes.too_long":        "Notas são muito longas (máximo 8000 caracteres)",
		"errors.person.birthday.range":        "Aniversário precisa de mês e dia completos",
		"errors.person.birthday.invalid":      "Aniversário não é um dia real nesse mês",
		"errors.person.birthday.needs_date":   "Ano de nascimento precisa de mês e dia",
		"errors.person.birth_year.range":      "Ano deve estar entre 1900 e 2100",
		"errors.person.base.too_many":         "Quinhentas pessoas bastam.",

		"errors.attr.label.blank":    "Toda linha extra precisa de um rótulo",
		"errors.attr.label.too_long": "Um rótulo extra é muito longo (máximo 80 caracteres)",
		"errors.attr.value.too_long": "Um valor extra é muito longo (máximo 500 caracteres)",
		"errors.attr.base.too_many":  "Cinquenta linhas extras por pessoa bastam.",

		"js.invalid_credentials":   "Email ou senha inválidos.",
		"js.wrong_password":        "Senha incorreta.",
		"js.theme_system":          "Tema: sistema",
		"js.theme_light":           "Tema: claro",
		"js.theme_dark":            "Tema: escuro",
		"js.theme_switch":          "clique para alternar",
		"js.delete_person_confirm": "Apagar %{name}? Não dá para desfazer.",
		"js.search_label":          "Buscar",
		"js.attr_label":            "Rótulo",
		"js.attr_value":            "Valor",
		"js.remove":                "Remover",
	},
}
