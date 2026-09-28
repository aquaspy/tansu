// Package i18n holds the en/pt string tables.
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

func TimeShort(l Locale, t time.Time) string {
	t = t.Local()
	if l == PT {
		return t.Format("02/01, 15:04")
	}
	return t.Format("Jan 2, 15:04")
}

var strings_ = map[Locale]map[string]string{
	EN: {
		"pwa.description": "Private email on your server.",

		"titles.app":      "Tansu Email",
		"titles.login":    "Sign in · Tansu Email",
		"titles.signup":   "Create account · Tansu Email",
		"titles.password": "Change password · Tansu Email",
		"titles.lock":     "Unlock · Tansu Email",
		"titles.tokens":   "API tokens · Tansu Email",
		"titles.agent":    "Link Assistant · Tansu Email",
		"titles.accounts": "Mailboxes · Tansu Email",

		"tokens.title":            "API tokens",
		"tokens.lede":             "Tokens let Tansu Assistant and other agents read and send mail through the API. A token keeps working while the app is locked. Treat it like a password. Mailbox passwords stay encrypted on this server and are never returned by the API.",
		"tokens.name_label":       "Name",
		"tokens.name_placeholder": "e.g. Tansu Assistant",
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
		"agent.lede":        "Tansu Assistant will list, read, and move mail in this account, and can send after you confirm in the chat. It can do that while this app is locked. Message text it reads is sent to the model.",
		"agent.session":     "This session: %{email}",
		"agent.confirm":     "Allow",
		"agent.bad_request": "This link is not valid.",

		"auth.sign_in":              "Sign in",
		"auth.sign_in_lede":         "Your mailboxes live on this server. Sign in to open them.",
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

		"app.unlock":        "Unlock",
		"app.unlock_lede":   "Signed in as %{email}. Enter your password to open the mail.",
		"app.lock":          "Lock",
		"app.auto_lock_on":  "Turn on auto lock",
		"app.auto_lock_off": "Turn off auto lock",
		"app.sign_out":      "Sign out",
		"app.cancel":        "Cancel",
		"app.more":          "More",
		"app.account":       "Tansu Account",
		"app.account_label": "Open Tansu Account",
		"app.offline":       "You're offline. Mail needs the network.",

		"mail.accounts":        "Mailboxes",
		"mail.accounts_lede":   "Connect a mailbox with IMAP and SMTP. The password is encrypted on this server. Messages stay on the mail server until you open them.",
		"mail.account":         "Mailbox",
		"mail.connect":         "Connect mailbox",
		"mail.edit":            "Edit",
		"mail.save":            "Save and test",
		"mail.display_name":    "Display name",
		"mail.from":            "From",
		"mail.username":        "Username",
		"mail.password":        "Password",
		"mail.password_keep":   "Leave blank to keep the saved password",
		"mail.imap_host":       "IMAP host",
		"mail.smtp_host":       "SMTP host",
		"mail.port":            "Port",
		"mail.security":        "Security",
		"mail.tls_none":        "None",
		"mail.last_ok":         "Last connection",
		"mail.remove_account":  "Remove mailbox",
		"mail.removed":         "Mailbox removed.",
		"mail.saved":           "Mailbox saved.",
		"mail.saved_error":     "Saved, but the connection failed.",
		"mail.secrets_missing": "Set KURA_SECRETS_KEY before connecting a mailbox.",
		"mail.inbox":           "Inbox",
		"mail.sent":            "Sent",
		"mail.trash":           "Trash",
		"mail.drafts":          "Drafts",
		"mail.junk":            "Junk",
		"mail.archive":         "Archive",
		"mail.compose":         "New message",
		"mail.search":          "Search",
		"mail.clear_search":    "Clear search",
		"mail.empty_accounts":  "No mailbox yet.",
		"mail.empty_connect":   "Connect a mailbox to read and send mail. The password stays encrypted on this server.",
		"mail.empty_list":      "Nothing in this folder.",
		"mail.empty_search":    "No messages match this search.",
		"mail.list_failed":     "Could not load this folder. The mail server did not answer.",
		"mail.search_failed":   "Search did not finish. The mail server did not answer.",
		"mail.search_capped":   "Showing the newest matches.",
		"mail.empty_reader":    "Choose a message.",
		"mail.no_subject":      "(no subject)",
		"mail.from_label":      "From",
		"mail.to":              "To",
		"mail.cc":              "Cc",
		"mail.bcc":             "Bcc",
		"mail.subject":         "Subject",
		"mail.date":            "Date",
		"mail.unread":          "Unread",
		"mail.mark_unread":     "Mark unread",
		"mail.marked_unread":   "Marked as unread.",
		"mail.reply":           "Reply",
		"mail.send":            "Send",
		"mail.sent_ok":         "Message sent.",
		"mail.trashed":         "Moved to Trash.",
		"mail.deleted":         "Message deleted.",
		"mail.trash_action":    "Trash",
		"mail.delete":          "Delete",
		"mail.delete_confirm":  "Delete this message permanently?",
		"mail.back":            "Back",
		"mail.prev":            "Previous",
		"mail.next":            "Next",
		"mail.unavailable":     "The mail server did not answer.",

		"js.invalid_credentials": "Invalid email or password.",
		"js.wrong_password":      "Wrong password.",
		"js.theme_system":        "Theme: system",
		"js.theme_light":         "Theme: light",
		"js.theme_dark":          "Theme: dark",
		"js.theme_switch":        "click to switch",
	},
	PT: {
		"pwa.description": "Email privado no seu servidor.",

		"titles.app":      "Tansu Email",
		"titles.login":    "Entrar · Tansu Email",
		"titles.signup":   "Criar conta · Tansu Email",
		"titles.password": "Mudar senha · Tansu Email",
		"titles.lock":     "Desbloquear · Tansu Email",
		"titles.tokens":   "Tokens de API · Tansu Email",
		"titles.agent":    "Ligar o Assistant · Tansu Email",
		"titles.accounts": "Caixas · Tansu Email",

		"tokens.title":            "Tokens de API",
		"tokens.lede":             "Tokens permitem que o Tansu Assistant e outros agentes leiam e enviem email pela API. Um token continua valendo com o app bloqueado. Trate como uma senha. As senhas das caixas ficam criptografadas neste servidor e a API nunca as devolve.",
		"tokens.name_label":       "Nome",
		"tokens.name_placeholder": "ex.: Tansu Assistant",
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
		"agent.lede":        "O Tansu Assistant vai listar, ler e mover o email desta conta, e só envia depois que você confirmar no chat. Ele faz isso mesmo com este app trancado. O texto que ele ler segue para o modelo.",
		"agent.session":     "Esta sessão: %{email}",
		"agent.confirm":     "Permitir",
		"agent.bad_request": "Este link não é válido.",

		"auth.sign_in":              "Entrar",
		"auth.sign_in_lede":         "As caixas de email ficam neste servidor. Entre para abri-las.",
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

		"app.unlock":        "Desbloquear",
		"app.unlock_lede":   "Conectado como %{email}. Digite a senha para abrir o email.",
		"app.lock":          "Bloquear",
		"app.auto_lock_on":  "Ligar bloqueio automático",
		"app.auto_lock_off": "Desligar bloqueio automático",
		"app.sign_out":      "Sair",
		"app.cancel":        "Cancelar",
		"app.more":          "Mais",
		"app.account":       "Tansu Account",
		"app.account_label": "Abrir o Tansu Account",
		"app.offline":       "Você está offline. O email precisa da rede.",

		"mail.accounts":        "Caixas",
		"mail.accounts_lede":   "Conecte uma caixa com IMAP e SMTP. A senha fica criptografada neste servidor. As mensagens continuam no servidor de email até você abri-las.",
		"mail.account":         "Caixa",
		"mail.connect":         "Conectar caixa",
		"mail.edit":            "Editar",
		"mail.save":            "Salvar e testar",
		"mail.display_name":    "Nome de exibição",
		"mail.from":            "De",
		"mail.username":        "Usuário",
		"mail.password":        "Senha",
		"mail.password_keep":   "Deixe em branco para manter a senha salva",
		"mail.imap_host":       "Servidor IMAP",
		"mail.smtp_host":       "Servidor SMTP",
		"mail.port":            "Porta",
		"mail.security":        "Segurança",
		"mail.tls_none":        "Nenhuma",
		"mail.last_ok":         "Última conexão",
		"mail.remove_account":  "Remover caixa",
		"mail.removed":         "Caixa removida.",
		"mail.saved":           "Caixa salva.",
		"mail.saved_error":     "Salva, mas a conexão falhou.",
		"mail.secrets_missing": "Defina KURA_SECRETS_KEY antes de conectar uma caixa.",
		"mail.inbox":           "Entrada",
		"mail.sent":            "Enviados",
		"mail.trash":           "Lixeira",
		"mail.drafts":          "Rascunhos",
		"mail.junk":            "Spam",
		"mail.archive":         "Arquivo",
		"mail.compose":         "Nova mensagem",
		"mail.search":          "Buscar",
		"mail.clear_search":    "Limpar busca",
		"mail.empty_accounts":  "Nenhuma caixa ainda.",
		"mail.empty_connect":   "Conecte uma caixa para ler e enviar email. A senha fica criptografada neste servidor.",
		"mail.empty_list":      "Nada nesta pasta.",
		"mail.empty_search":    "Nenhuma mensagem combina com esta busca.",
		"mail.list_failed":     "Não foi possível carregar esta pasta. O servidor de email não respondeu.",
		"mail.search_failed":   "A busca não terminou. O servidor de email não respondeu.",
		"mail.search_capped":   "Mostrando as correspondências mais recentes.",
		"mail.empty_reader":    "Escolha uma mensagem.",
		"mail.no_subject":      "(sem assunto)",
		"mail.from_label":      "De",
		"mail.to":              "Para",
		"mail.cc":              "Cc",
		"mail.bcc":             "Cco",
		"mail.subject":         "Assunto",
		"mail.date":            "Data",
		"mail.unread":          "Não lida",
		"mail.mark_unread":     "Marcar como não lida",
		"mail.marked_unread":   "Marcada como não lida.",
		"mail.reply":           "Responder",
		"mail.send":            "Enviar",
		"mail.sent_ok":         "Mensagem enviada.",
		"mail.trashed":         "Movida para a Lixeira.",
		"mail.deleted":         "Mensagem apagada.",
		"mail.trash_action":    "Lixeira",
		"mail.delete":          "Apagar",
		"mail.delete_confirm":  "Apagar esta mensagem para sempre?",
		"mail.back":            "Voltar",
		"mail.prev":            "Anterior",
		"mail.next":            "Próxima",
		"mail.unavailable":     "O servidor de email não respondeu.",

		"js.invalid_credentials": "Email ou senha inválidos.",
		"js.wrong_password":      "Senha incorreta.",
		"js.theme_system":        "Tema: sistema",
		"js.theme_light":         "Tema: claro",
		"js.theme_dark":          "Tema: escuro",
		"js.theme_switch":        "clique para alternar",
	},
}
