// Package i18n holds the en/pt string tables ported from
// config/locales. Keys use the same dot paths as the Rails app.
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

var strings_ = map[Locale]map[string]string{
	EN: {
		"pwa.description": "Simple private notes as a PWA.",

		"titles.app":      "TansuNotes",
		"titles.login":    "Sign in · TansuNotes",
		"titles.signup":   "Create account · TansuNotes",
		"titles.password": "Change password · TansuNotes",
		"titles.lock":     "Unlock · TansuNotes",
		"titles.shared":   "%{title} · TansuNotes",
		"titles.tokens":   "API tokens · TansuNotes",

		"api.invalid_folder": "Pick a folder to clear.",

		"tokens.title":            "API tokens",
		"tokens.lede":             "Tokens let AI agents and other apps manage your notes through the API. A token has full access to your notes and folders, and keeps working while the app is locked. Treat it like a password.",
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

		"auth.sign_in":              "Sign in",
		"auth.sign_in_lede":         "Your notes live on this server. Sign in to open them.",
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
		"app.unlock_lede":           "Signed in as %{email}. Enter your password to show the notes.",
		"app.lock":                  "Lock",
		"app.auto_lock_on":          "Turn on auto lock",
		"app.auto_lock_off":         "Turn off auto lock",
		"app.sign_out":              "Sign out",
		"app.export":                "Export",
		"app.import":                "Import",
		"app.import_done":           "Imported %{count} notes.",
		"app.import_invalid":        "That file is not a valid TansuNotes, Notesnook, or Standard Notes export.",
		"app.new_note":              "New note",
		"app.search":                "Search",
		"app.notes":                 "Notes",
		"app.folders":               "Folders",
		"app.list":                  "List",
		"app.focus":                 "Focus (Ctrl+\\)",
		"app.folder":                "Folder",
		"app.choose_folder":         "Choose folder",
		"app.delete":                "Delete",
		"app.cancel":                "Cancel",
		"app.rename":                "Rename",
		"app.rename_folder":         "Rename folder",
		"app.delete_folder":         "Delete folder",
		"app.clear_inbox":           "Clear Inbox",
		"app.folder_cleared":        "Removed %{count} notes.",
		"app.folder_renamed":        "Folder renamed.",
		"app.folder_rename_invalid": "That folder name can't be used.",
		"app.empty_editor":          "Pick a note or create a new one.",
		"app.write_placeholder":     "Write. The first line is the title.",
		"app.more":                  "More",
		"app.account":               "Tansu Account",
		"app.account_label":         "Open Tansu Account",
		"app.share":                 "Share",
		"app.share_lede":            "Anyone with the link can read this note. They don't need an account.",
		"app.share_create":          "Create link",
		"app.share_new":             "New link",
		"app.share_stop":            "Remove link",
		"app.copy":                  "Copy",
		"app.copied":                "Copied",
		"app.shared_from":           "Shared from TansuNotes",
		"app.offline":               "You're offline. You can read, but changes won't save.",

		"js.invalid_credentials":   "Invalid email or password.",
		"js.wrong_password":        "Wrong password.",
		"js.all":                   "All",
		"js.inbox":                 "Inbox",
		"js.untitled":              "Untitled",
		"js.empty_list":            "No notes here.",
		"js.saving":                "Saving…",
		"js.saved":                 "Saved",
		"js.offline":               "Offline",
		"js.delete_confirm":        "Delete this note?",
		"js.delete_folder_confirm": "Delete “%{name}” and all notes in it?",
		"js.delete_inbox_confirm":  "Delete all notes in Inbox? Inbox itself will stay.",
		"js.theme_system":          "Theme: system",
		"js.theme_light":           "Theme: light",
		"js.theme_dark":            "Theme: dark",
		"js.theme_switch":          "click to switch",
	},
	PT: {
		"pwa.description": "Notas simples como PWA.",

		"titles.app":      "TansuNotes",
		"titles.login":    "Entrar · TansuNotes",
		"titles.signup":   "Criar conta · TansuNotes",
		"titles.password": "Mudar senha · TansuNotes",
		"titles.lock":     "Desbloquear · TansuNotes",
		"titles.shared":   "%{title} · TansuNotes",
		"titles.tokens":   "Tokens de API · TansuNotes",

		"api.invalid_folder": "Escolha uma pasta para limpar.",

		"tokens.title":            "Tokens de API",
		"tokens.lede":             "Tokens permitem que agentes de IA e outros apps gerenciem suas notas pela API. Um token tem acesso total às notas e pastas e continua valendo com o app bloqueado. Trate como uma senha.",
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

		"auth.sign_in":              "Entrar",
		"auth.sign_in_lede":         "As notas ficam neste servidor. Entre para abri-las.",
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
		"app.unlock_lede":           "Conectado como %{email}. Digite a senha para ver as notas.",
		"app.lock":                  "Bloquear",
		"app.auto_lock_on":          "Ligar bloqueio automático",
		"app.auto_lock_off":         "Desligar bloqueio automático",
		"app.sign_out":              "Sair",
		"app.export":                "Exportar",
		"app.import":                "Importar",
		"app.import_done":           "Importadas %{count} notas.",
		"app.import_invalid":        "Esse arquivo não é um export válido do TansuNotes, Notesnook ou Standard Notes.",
		"app.new_note":              "Nova nota",
		"app.search":                "Buscar",
		"app.notes":                 "Notas",
		"app.folders":               "Pastas",
		"app.list":                  "Lista",
		"app.focus":                 "Foco (Ctrl+\\)",
		"app.folder":                "Pasta",
		"app.choose_folder":         "Escolher pasta",
		"app.delete":                "Apagar",
		"app.cancel":                "Cancelar",
		"app.rename":                "Renomear",
		"app.rename_folder":         "Renomear pasta",
		"app.clear_inbox":           "Limpar Inbox",
		"app.delete_folder":         "Apagar pasta",
		"app.folder_cleared":        "Removidas %{count} notas.",
		"app.folder_renamed":        "Pasta renomeada.",
		"app.folder_rename_invalid": "Esse nome de pasta não pode ser usado.",
		"app.empty_editor":          "Escolha uma nota ou crie uma nova.",
		"app.write_placeholder":     "Escreva. A primeira linha é o título.",
		"app.more":                  "Mais",
		"app.account":               "Tansu Account",
		"app.account_label":         "Abrir o Tansu Account",
		"app.share":                 "Compartilhar",
		"app.share_lede":            "Quem tiver o link lê a nota. Não precisa de conta.",
		"app.share_create":          "Criar link",
		"app.share_new":             "Novo link",
		"app.share_stop":            "Remover link",
		"app.copy":                  "Copiar",
		"app.copied":                "Copiado",
		"app.shared_from":           "Compartilhado pelo TansuNotes",
		"app.offline":               "Você está offline. Pode ler, mas não salvar.",

		"js.invalid_credentials":   "Email ou senha inválidos.",
		"js.wrong_password":        "Senha incorreta.",
		"js.all":                   "Tudo",
		"js.inbox":                 "Inbox",
		"js.untitled":              "Sem título",
		"js.empty_list":            "Nenhuma nota aqui.",
		"js.saving":                "Salvando…",
		"js.saved":                 "Salvo",
		"js.offline":               "Offline",
		"js.delete_confirm":        "Apagar esta nota?",
		"js.delete_folder_confirm": "Apagar “%{name}” e todas as notas dela?",
		"js.delete_inbox_confirm":  "Apagar todas as notas da Inbox? A Inbox em si continua.",
		"js.theme_system":          "Tema: sistema",
		"js.theme_light":           "Tema: claro",
		"js.theme_dark":            "Tema: escuro",
		"js.theme_switch":          "clique para alternar",
	},
}
