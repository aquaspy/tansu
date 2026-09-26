// Package i18n holds the en/pt string tables. Keys use the same dot
// paths as the sibling apps.
package i18n

import (
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

var _ = itoa

var strings_ = map[Locale]map[string]string{
	EN: {
		"pwa.description": "One login for every Tansu app.",

		"titles.app":      "TansuAccount",
		"titles.login":    "Sign in · TansuAccount",
		"titles.signup":   "Create account · TansuAccount",
		"titles.password": "Change password · TansuAccount",
		"titles.lock":     "Unlock · TansuAccount",

		"auth.sign_in":              "Sign in",
		"auth.sign_in_lede":         "One account for the whole suite. Sign in to open your apps.",
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
		"auth.signup_closed":        "New accounts are turned off on this server.",
		"auth.email_invalid":        "That email doesn't look right.",
		"auth.email_taken":          "That email is already registered.",
		"auth.password_too_short":   "Password is too short (minimum 8 characters).",
		"auth.password_mismatch":    "Password confirmation doesn't match.",

		"app.unlock":        "Unlock",
		"app.unlock_lede":   "Signed in as %{email}. Enter your password to show your account.",
		"app.lock":          "Lock",
		"app.auto_lock_on":  "Turn on auto lock",
		"app.auto_lock_off": "Turn off auto lock",
		"app.sign_out":      "Sign out",
		"app.more":          "More",
		"app.cancel":        "Cancel",
		"app.offline":       "You're offline. The hub needs a connection to open your apps.",

		"hub.title":       "Your fox guards the suite",
		"hub.tails":       "%{count} of %{max} tails",
		"hub.apps":        "Apps",
		"hub.open":        "Open",
		"hub.linked":      "Connected",
		"hub.not_linked":  "Not connected yet",
		"hub.empty_title": "No apps yet",
		"hub.empty_lede":  "Register suite apps in KURA_CLIENTS_JSON and restart to see them here.",

		"js.invalid_credentials": "Invalid email or password.",
		"js.wrong_password":      "Wrong password.",
		"js.theme_system":        "Theme: system",
		"js.theme_light":         "Theme: light",
		"js.theme_dark":          "Theme: dark",
		"js.theme_switch":        "click to switch",
	},
	PT: {
		"pwa.description": "Um login para todos os apps Tansu.",

		"titles.app":      "TansuAccount",
		"titles.login":    "Entrar · TansuAccount",
		"titles.signup":   "Criar conta · TansuAccount",
		"titles.password": "Mudar senha · TansuAccount",
		"titles.lock":     "Desbloquear · TansuAccount",

		"auth.sign_in":              "Entrar",
		"auth.sign_in_lede":         "Uma conta para a suíte inteira. Entre para abrir seus apps.",
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
		"auth.signup_closed":        "Novas contas estão desligadas neste servidor.",
		"auth.email_invalid":        "Esse email não parece certo.",
		"auth.email_taken":          "Esse email já está registrado.",
		"auth.password_too_short":   "Senha muito curta (mínimo 8 caracteres).",
		"auth.password_mismatch":    "A confirmação não confere com a senha.",

		"app.unlock":        "Desbloquear",
		"app.unlock_lede":   "Conectado como %{email}. Digite a senha para ver sua conta.",
		"app.lock":          "Bloquear",
		"app.auto_lock_on":  "Ligar bloqueio automático",
		"app.auto_lock_off": "Desligar bloqueio automático",
		"app.sign_out":      "Sair",
		"app.more":          "Mais",
		"app.cancel":        "Cancelar",
		"app.offline":       "Você está offline. O hub precisa de conexão para abrir seus apps.",

		"hub.title":       "Sua raposa guarda a suíte",
		"hub.tails":       "%{count} de %{max} caudas",
		"hub.apps":        "Apps",
		"hub.open":        "Abrir",
		"hub.linked":      "Conectado",
		"hub.not_linked":  "Ainda não conectado",
		"hub.empty_title": "Nenhum app ainda",
		"hub.empty_lede":  "Registre os apps da suíte em KURA_CLIENTS_JSON e reinicie para vê-los aqui.",

		"js.invalid_credentials": "Email ou senha inválidos.",
		"js.wrong_password":      "Senha incorreta.",
		"js.theme_system":        "Tema: sistema",
		"js.theme_light":         "Tema: claro",
		"js.theme_dark":          "Tema: escuro",
		"js.theme_switch":        "clique para alternar",
	},
}
