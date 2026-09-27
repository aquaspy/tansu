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

// FromCookie prefers an explicit kura_locale cookie over Accept-Language.
func FromCookie(header, cookie string) Locale {
	switch strings.ToLower(strings.TrimSpace(cookie)) {
	case "pt", "pt-br":
		return PT
	case "en":
		return EN
	default:
		return FromHeader(header)
	}
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

		"titles.app":            "TansuAccount",
		"titles.login":          "Sign in · TansuAccount",
		"titles.signup":         "Create account · TansuAccount",
		"titles.signup_sent":    "Check your email · TansuAccount",
		"titles.signup_confirm": "Confirm account · TansuAccount",
		"titles.forgot":         "Forgot password · TansuAccount",
		"titles.reset":          "Reset password · TansuAccount",
		"titles.password":       "Change password · TansuAccount",
		"titles.lock":           "Unlock · TansuAccount",

		"auth.sign_in":               "Sign in",
		"auth.sign_in_lede":          "One account for the whole suite. Sign in to open your apps.",
		"auth.email":                 "Email",
		"auth.password":              "Password",
		"auth.confirm_password":      "Confirm password",
		"auth.current_password":      "Current password",
		"auth.new_password":          "New password",
		"auth.confirm_new_password":  "Confirm new password",
		"auth.change_password":       "Change password",
		"auth.change_password_lede":  "The new password is used to sign in and to unlock the app.",
		"auth.password_changed":      "Password changed.",
		"auth.no_account":            "No account?",
		"auth.create_one":            "Create one",
		"auth.create_account":        "Create account",
		"auth.signup_lede":           "There is no password recovery. If you lose it, ask whoever runs the server.",
		"auth.signup_lede_mail":      "If you forget the password, use Forgot password? on the sign-in page.",
		"auth.forgot_password":       "Forgot password?",
		"auth.forgot_title":          "Reset your password",
		"auth.forgot_lede":           "Enter the email on the account. If it matches, we send a link.",
		"auth.forgot_submit":         "Send reset link",
		"auth.forgot_sent_title":     "Check your email",
		"auth.forgot_sent_lede":      "If an account exists for %{email}, we sent a link to choose a new password. If you didn't ask for this, ignore the message.",
		"auth.reset_title":           "Choose a new password",
		"auth.reset_lede":            "This signs you out of Tansu Account on every device.",
		"auth.reset_submit":          "Save password",
		"auth.reset_invalid":         "That reset link is invalid or expired.",
		"auth.reset_done":            "Password changed. Sign in with the new one.",
		"auth.reset_failed":          "Could not start the reset. Try again.",
		"auth.reset_mail_failed":     "Could not send the reset email. Try again.",
		"auth.reset_subject":         "Reset your Tansu password",
		"auth.reset_text":            "Reset your Tansu password within 1 hour:\n%{url}\n\nIf you didn't ask for this, ignore this message. Your password stays the same until you choose a new one.\n",
		"auth.reset_html":            "<p>Reset your Tansu password within 1 hour:</p><p><a href=\"%{url}\">Choose a new password</a></p><p>If you didn't ask for this, ignore this message. Your password stays the same until you choose a new one.</p>",
		"auth.have_account":          "Already have an account?",
		"auth.theme":                 "Theme",
		"auth.language":              "Language",
		"auth.too_many":              "Too many attempts. Wait a moment.",
		"auth.signup_closed":         "New accounts are turned off on this server.",
		"auth.email_invalid":         "That email doesn't look right.",
		"auth.email_taken":           "That email is already registered.",
		"auth.password_too_short":    "Password is too short (minimum 8 characters).",
		"auth.password_mismatch":     "Password confirmation doesn't match.",
		"auth.signup_sent_title":     "Check your email",
		"auth.signup_sent_lede":      "We sent a confirmation link to %{email}. The account is created when you open it and enter your password. If you didn't ask for this, ignore the message.",
		"auth.signup_confirm_title":  "Confirm your account",
		"auth.signup_confirm_lede":   "Enter the password you chose. This creates the account.",
		"auth.signup_confirm_submit": "Confirm and create account",
		"auth.confirm_invalid":       "That confirmation link is invalid or expired.",
		"auth.confirm_locked":        "Too many password attempts. Request a new confirmation email.",
		"auth.mail_failed":           "Could not send the confirmation email. Try again.",
		"auth.signup_failed":         "Could not start the signup. Try again.",
		"auth.confirm_subject":       "Confirm your Tansu account",
		"auth.confirm_text":          "Confirm your Tansu account within 24 hours:\n%{url}\n\nIf you didn't ask for this, ignore this message. No account is created until you confirm with your password.\n",
		"auth.confirm_html":          "<p>Confirm your Tansu account within 24 hours:</p><p><a href=\"%{url}\">Create account</a></p><p>If you didn't ask for this, ignore this message. No account is created until you confirm with your password.</p>",

		"app.unlock":        "Unlock",
		"app.unlock_lede":   "Signed in as %{email}. Enter your password to show your account.",
		"app.lock":          "Lock",
		"app.auto_lock_on":  "Turn on auto lock",
		"app.auto_lock_off": "Turn off auto lock",
		"app.sign_out":      "Sign out",
		"app.more":          "More",
		"app.cancel":        "Cancel",
		"app.offline":       "You're offline. The hub needs a connection to open your apps.",

		"hub.title":       "The tansu holds the suite",
		"hub.drawers":     "%{count}/%{max}",
		"hub.apps":        "Apps",
		"hub.open":        "Open",
		"hub.connect":     "Connect",
		"hub.linked":      "Connected",
		"hub.not_linked":  "Not connected yet",
		"hub.empty_title": "No apps yet",
		"hub.empty_lede":  "When the apps are connected, they show up here.",
		"hub.link_title":  "Link apps to Assistant",
		"hub.link_lede":   "Tansu Assistant can read and change Notes, Calendar, Spend, and People once you link them. You confirm each app.",
		"hub.link_action": "Connect apps",

		"js.invalid_credentials": "Invalid email or password.",
		"js.wrong_password":      "Wrong password.",
		"js.theme_system":        "Theme: system",
		"js.theme_light":         "Theme: light",
		"js.theme_dark":          "Theme: dark",
		"js.theme_switch":        "click to switch",
	},
	PT: {
		"pwa.description": "Um login para todos os apps Tansu.",

		"titles.app":            "TansuAccount",
		"titles.login":          "Entrar · TansuAccount",
		"titles.signup":         "Criar conta · TansuAccount",
		"titles.signup_sent":    "Veja seu email · TansuAccount",
		"titles.signup_confirm": "Confirmar conta · TansuAccount",
		"titles.forgot":         "Esqueci a senha · TansuAccount",
		"titles.reset":          "Redefinir senha · TansuAccount",
		"titles.password":       "Mudar senha · TansuAccount",
		"titles.lock":           "Desbloquear · TansuAccount",

		"auth.sign_in":               "Entrar",
		"auth.sign_in_lede":          "Uma conta para a suíte inteira. Entre para abrir seus apps.",
		"auth.email":                 "Email",
		"auth.password":              "Senha",
		"auth.confirm_password":      "Confirmar senha",
		"auth.current_password":      "Senha atual",
		"auth.new_password":          "Nova senha",
		"auth.confirm_new_password":  "Confirmar nova senha",
		"auth.change_password":       "Mudar senha",
		"auth.change_password_lede":  "A senha nova vale para entrar e para desbloquear o app.",
		"auth.password_changed":      "Senha alterada.",
		"auth.no_account":            "Sem conta?",
		"auth.create_one":            "Criar uma",
		"auth.create_account":        "Criar conta",
		"auth.signup_lede":           "Não existe recuperação de senha. Se perder, peça a quem administra o servidor.",
		"auth.signup_lede_mail":      "Se esquecer a senha, use Esqueceu a senha? na página de entrar.",
		"auth.forgot_password":       "Esqueceu a senha?",
		"auth.forgot_title":          "Redefinir senha",
		"auth.forgot_lede":           "Digite o email da conta. Se existir, enviamos um link.",
		"auth.forgot_submit":         "Enviar link",
		"auth.forgot_sent_title":     "Veja seu email",
		"auth.forgot_sent_lede":      "Se existir uma conta para %{email}, enviamos um link para escolher uma senha nova. Se não foi você, ignore a mensagem.",
		"auth.reset_title":           "Escolha uma senha nova",
		"auth.reset_lede":            "Isso encerra a sessão do Tansu Account em todos os aparelhos.",
		"auth.reset_submit":          "Salvar senha",
		"auth.reset_invalid":         "Esse link de redefinição é inválido ou expirou.",
		"auth.reset_done":            "Senha alterada. Entre com a nova.",
		"auth.reset_failed":          "Não foi possível iniciar a redefinição. Tente de novo.",
		"auth.reset_mail_failed":     "Não foi possível enviar o email de redefinição. Tente de novo.",
		"auth.reset_subject":         "Redefina sua senha Tansu",
		"auth.reset_text":            "Redefina sua senha Tansu em até 1 hora:\n%{url}\n\nSe não foi você, ignore esta mensagem. A senha só muda quando você escolher uma nova.\n",
		"auth.reset_html":            "<p>Redefina sua senha Tansu em até 1 hora:</p><p><a href=\"%{url}\">Escolher senha nova</a></p><p>Se não foi você, ignore esta mensagem. A senha só muda quando você escolher uma nova.</p>",
		"auth.have_account":          "Já tem conta?",
		"auth.theme":                 "Tema",
		"auth.language":              "Idioma",
		"auth.too_many":              "Muitas tentativas. Espere um pouco.",
		"auth.signup_closed":         "Novas contas estão desligadas neste servidor.",
		"auth.email_invalid":         "Esse email não parece certo.",
		"auth.email_taken":           "Esse email já está registrado.",
		"auth.password_too_short":    "Senha muito curta (mínimo 8 caracteres).",
		"auth.password_mismatch":     "A confirmação não confere com a senha.",
		"auth.signup_sent_title":     "Veja seu email",
		"auth.signup_sent_lede":      "Enviamos um link de confirmação para %{email}. A conta só é criada quando você abre o link e digita a senha. Se não foi você, ignore a mensagem.",
		"auth.signup_confirm_title":  "Confirme sua conta",
		"auth.signup_confirm_lede":   "Digite a senha que você escolheu. Isso cria a conta.",
		"auth.signup_confirm_submit": "Confirmar e criar conta",
		"auth.confirm_invalid":       "Esse link de confirmação é inválido ou expirou.",
		"auth.confirm_locked":        "Muitas tentativas de senha. Peça um novo email de confirmação.",
		"auth.mail_failed":           "Não foi possível enviar o email de confirmação. Tente de novo.",
		"auth.signup_failed":         "Não foi possível iniciar o cadastro. Tente de novo.",
		"auth.confirm_subject":       "Confirme sua conta Tansu",
		"auth.confirm_text":          "Confirme sua conta Tansu em até 24 horas:\n%{url}\n\nSe não foi você, ignore esta mensagem. Nenhuma conta é criada até você confirmar com a senha.\n",
		"auth.confirm_html":          "<p>Confirme sua conta Tansu em até 24 horas:</p><p><a href=\"%{url}\">Criar conta</a></p><p>Se não foi você, ignore esta mensagem. Nenhuma conta é criada até você confirmar com a senha.</p>",

		"app.unlock":        "Desbloquear",
		"app.unlock_lede":   "Conectado como %{email}. Digite a senha para ver sua conta.",
		"app.lock":          "Bloquear",
		"app.auto_lock_on":  "Ligar bloqueio automático",
		"app.auto_lock_off": "Desligar bloqueio automático",
		"app.sign_out":      "Sair",
		"app.more":          "Mais",
		"app.cancel":        "Cancelar",
		"app.offline":       "Você está offline. O hub precisa de conexão para abrir seus apps.",

		"hub.title":       "O tansu guarda a suíte",
		"hub.drawers":     "%{count}/%{max}",
		"hub.apps":        "Apps",
		"hub.open":        "Abrir",
		"hub.connect":     "Conectar",
		"hub.linked":      "Conectado",
		"hub.not_linked":  "Ainda não conectado",
		"hub.empty_title": "Nenhum app ainda",
		"hub.empty_lede":  "Quando os apps estiverem ligados, eles aparecem aqui.",
		"hub.link_title":  "Ligue apps ao Assistant",
		"hub.link_lede":   "O Tansu Assistant pode ler e alterar Notas, Calendário, Gastos e Pessoas depois que você liga cada um. Você confirma cada app.",
		"hub.link_action": "Conectar apps",

		"js.invalid_credentials": "Email ou senha inválidos.",
		"js.wrong_password":      "Senha incorreta.",
		"js.theme_system":        "Tema: sistema",
		"js.theme_light":         "Tema: claro",
		"js.theme_dark":          "Tema: escuro",
		"js.theme_switch":        "clique para alternar",
	},
}
