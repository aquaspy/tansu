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

// MonthLabel mirrors date.formats.month: "September 2026" / "setembro de 2026".
func MonthLabel(l Locale, t time.Time) string {
	if l == PT {
		return ptMonths[t.Month()-1] + " de " + itoa(t.Year())
	}
	return t.Format("January 2006")
}

// DayLabel mirrors date.formats.day: "Monday, September 7" /
// "segunda-feira, 7 de setembro".
func DayLabel(l Locale, t time.Time) string {
	if l == PT {
		return ptDays[t.Weekday()] + ", " + itoa(t.Day()) + " de " + ptMonths[t.Month()-1]
	}
	return t.Format("Monday, January 2")
}

// AbbrDayNames mirrors date.abbr_day_names indexed by time.Weekday
// (Sunday=0).
func AbbrDayNames(l Locale) [7]string {
	if l == PT {
		return [7]string{"Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"}
	}
	return [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
}

// MonthNames mirrors date.month_names (January..December /
// janeiro..dezembro) for the birthday month picker.
func MonthNames(l Locale) [12]string {
	if l == PT {
		return [12]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho",
			"julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}
	}
	return [12]string{"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December"}
}

var ptMonths = [12]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}

var ptDays = [7]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira",
	"quinta-feira", "sexta-feira", "sábado"}

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
		"pwa.description": "A quiet personal calendar as a PWA.",

		"titles.app":      "TansuCalendar",
		"titles.login":    "Sign in · TansuCalendar",
		"titles.signup":   "Create account · TansuCalendar",
		"titles.password": "Change password · TansuCalendar",
		"titles.lock":     "Unlock · TansuCalendar",
		"titles.tokens":   "API tokens · TansuCalendar",

		"api.invalid_date": "Dates must use YYYY-MM-DD.",

		"tokens.title":            "API tokens",
		"tokens.lede":             "Tokens let AI agents and other apps manage your calendar through the API. A token has full access to your events and birthdays, and keeps working while the app is locked. Treat it like a password.",
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
		"auth.sign_in_lede":         "Your calendar lives on this server. Sign in to open it.",
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

		"app.unlock":         "Unlock",
		"app.unlock_lede":    "Signed in as %{email}. Enter your password to show the calendar.",
		"app.lock":           "Lock",
		"app.auto_lock_on":   "Turn on auto lock",
		"app.auto_lock_off":  "Turn off auto lock",
		"app.sign_out":       "Sign out",
		"app.cancel":         "Cancel",
		"app.delete":         "Delete",
		"app.save":           "Save",
		"app.more":           "More",
		"app.today":          "Today",
		"app.previous_month": "Previous month",
		"app.next_month":     "Next month",
		"app.new_item":       "Add",
		"app.new_event":      "Event",
		"app.new_birthday":   "Birthday",
		"app.edit_event":     "Edit event",
		"app.edit_birthday":  "Edit birthday",
		"app.empty_day":      "Nothing on this day.",
		"app.all_day":        "All day",
		"app.title":          "Title",
		"app.notes":          "Notes",
		"app.starts":         "Starts",
		"app.ends":           "Ends",
		"app.start_time":     "Start time",
		"app.end_time":       "End time",
		"app.name":           "Name",
		"app.month":          "Month",
		"app.day":            "Day",
		"app.year":           "Year",
		"app.year_hint":      "Optional. Used to show age.",
		"app.emoji":          "Emoji",
		"app.repeat":         "Repeat",
		"app.repeat_none":    "Never",
		"app.repeat_daily":   "Daily",
		"app.repeat_weekly":  "Weekly",
		"app.repeat_monthly": "Monthly",
		"app.repeat_yearly":  "Yearly",
		"app.repeat_until":   "Until",
		"app.repeat_hint":    "Blank repeats forever. Editing changes every occurrence.",
		"app.holidays":       "Holidays",
		"app.holidays_lede":  "Pick the countries whose public holidays you want on the month.",
		"app.export":         "Export",
		"app.import":         "Import",
		"app.import_done":    "Imported %{count} items.",
		"app.import_invalid": "That file is not a valid TansuCalendar export.",
		"app.offline":        "You're offline. You can open months you've already viewed, but changes won't save.",
		"app.overflow":       "+%{count}",

		"countries.BR": "Brazil",
		"countries.US": "United States",
		"countries.SI": "Slovenia",
		"countries.CZ": "Czechia",

		"holidays.br.new_year":        "New Year's Day",
		"holidays.br.carnival":        "Carnival",
		"holidays.br.good_friday":     "Good Friday",
		"holidays.br.easter":          "Easter",
		"holidays.br.tiradentes":      "Tiradentes",
		"holidays.br.labor":           "Labour Day",
		"holidays.br.corpus_christi":  "Corpus Christi",
		"holidays.br.independence":    "Independence Day",
		"holidays.br.aparecida":       "Our Lady of Aparecida",
		"holidays.br.finados":         "All Souls' Day",
		"holidays.br.republic":        "Republic Day",
		"holidays.br.black_awareness": "Black Awareness Day",
		"holidays.br.christmas":       "Christmas",

		"holidays.us.new_year":     "New Year's Day",
		"holidays.us.mlk":          "Martin Luther King Jr. Day",
		"holidays.us.presidents":   "Presidents' Day",
		"holidays.us.memorial":     "Memorial Day",
		"holidays.us.juneteenth":   "Juneteenth",
		"holidays.us.independence": "Independence Day",
		"holidays.us.labor":        "Labor Day",
		"holidays.us.columbus":     "Columbus Day",
		"holidays.us.veterans":     "Veterans Day",
		"holidays.us.thanksgiving": "Thanksgiving",
		"holidays.us.christmas":    "Christmas",

		"holidays.si.new_year":      "New Year's Day",
		"holidays.si.new_year_2":    "New Year holiday",
		"holidays.si.preseren":      "Prešeren Day",
		"holidays.si.easter":        "Easter",
		"holidays.si.easter_monday": "Easter Monday",
		"holidays.si.uprising":      "Day of Uprising Against Occupation",
		"holidays.si.labor":         "Labour Day",
		"holidays.si.labor_2":       "Labour Day",
		"holidays.si.pentecost":     "Whit Sunday",
		"holidays.si.statehood":     "Statehood Day",
		"holidays.si.assumption":    "Assumption",
		"holidays.si.reformation":   "Reformation Day",
		"holidays.si.all_saints":    "All Saints' Day",
		"holidays.si.christmas":     "Christmas",
		"holidays.si.independence":  "Independence and Unity Day",

		"holidays.cz.new_year":        "New Year's Day",
		"holidays.cz.good_friday":     "Good Friday",
		"holidays.cz.easter_monday":   "Easter Monday",
		"holidays.cz.labor":           "Labour Day",
		"holidays.cz.liberation":      "Liberation Day",
		"holidays.cz.cyril_methodius": "Saints Cyril and Methodius",
		"holidays.cz.jan_hus":         "Jan Hus Day",
		"holidays.cz.statehood":       "Czech Statehood Day",
		"holidays.cz.czechoslovak":    "Independent Czechoslovak State Day",
		"holidays.cz.freedom":         "Struggle for Freedom and Democracy",
		"holidays.cz.christmas_eve":   "Christmas Eve",
		"holidays.cz.christmas":       "Christmas",
		"holidays.cz.st_stephen":      "St Stephen's Day",

		// Validation messages mirror full_messages from the Rails app.
		"errors.event.title.blank":               "Title can't be blank",
		"errors.event.title.too_long":            "Title is too long (200 characters max)",
		"errors.event.body.too_long":             "Notes are too long (8000 characters max)",
		"errors.event.starts_on.blank":           "Start date can't be blank",
		"errors.event.ends_on.blank":             "End date can't be blank",
		"errors.event.ends_on.before_start":      "End date can't be before the start date",
		"errors.event.ends_on.too_long":          "End date can't span more than a year",
		"errors.event.starts_at.blank":           "Start time can't be blank",
		"errors.event.ends_at.before_start":      "End time can't be before the start time",
		"errors.event.base.too_many":             "Two thousand events is enough.",
		"errors.event.emoji.too_long":            "Emoji is too long",
		"errors.event.repeat.invalid":            "Repeat isn't valid",
		"errors.event.repeat_until.invalid":      "Until isn't a valid date",
		"errors.event.repeat_until.before_start": "Until can't be before the start date",

		"errors.birthday.name.blank":     "Name can't be blank",
		"errors.birthday.name.too_long":  "Name is too long (200 characters max)",
		"errors.birthday.body.too_long":  "Notes are too long (8000 characters max)",
		"errors.birthday.month.range":    "Month must be between 1 and 12",
		"errors.birthday.day.range":      "Day must be between 1 and 31",
		"errors.birthday.day.invalid":    "Day isn't a real day in that month",
		"errors.birthday.year.range":     "Year must be between 1900 and 2100",
		"errors.birthday.base.too_many":  "Five hundred birthdays is enough.",
		"errors.birthday.emoji.too_long": "Emoji is too long",

		"js.invalid_credentials":         "Invalid email or password.",
		"js.wrong_password":              "Wrong password.",
		"js.theme_system":                "Theme: system",
		"js.theme_light":                 "Theme: light",
		"js.theme_dark":                  "Theme: dark",
		"js.theme_switch":                "click to switch",
		"js.delete_event_confirm":        "Delete this event?",
		"js.delete_event_series_confirm": "Delete this event and all its repetitions?",
		"js.delete_birthday_confirm":     "Delete this birthday?",
	},
	PT: {
		"pwa.description": "Um calendário pessoal calmo como PWA.",

		"titles.app":      "TansuCalendar",
		"titles.login":    "Entrar · TansuCalendar",
		"titles.signup":   "Criar conta · TansuCalendar",
		"titles.password": "Mudar senha · TansuCalendar",
		"titles.lock":     "Desbloquear · TansuCalendar",
		"titles.tokens":   "Tokens de API · TansuCalendar",

		"api.invalid_date": "As datas devem usar AAAA-MM-DD.",

		"tokens.title":            "Tokens de API",
		"tokens.lede":             "Tokens permitem que agentes de IA e outros apps gerenciem seu calendário pela API. Um token tem acesso total aos eventos e aniversários e continua valendo com o app bloqueado. Trate como uma senha.",
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
		"auth.sign_in_lede":         "Seu calendário fica neste servidor. Entre para abri-lo.",
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

		"app.unlock":         "Desbloquear",
		"app.unlock_lede":    "Conectado como %{email}. Digite a senha para ver o calendário.",
		"app.lock":           "Bloquear",
		"app.auto_lock_on":   "Ligar bloqueio automático",
		"app.auto_lock_off":  "Desligar bloqueio automático",
		"app.sign_out":       "Sair",
		"app.cancel":         "Cancelar",
		"app.delete":         "Apagar",
		"app.save":           "Salvar",
		"app.more":           "Mais",
		"app.today":          "Hoje",
		"app.previous_month": "Mês anterior",
		"app.next_month":     "Próximo mês",
		"app.new_item":       "Adicionar",
		"app.new_event":      "Evento",
		"app.new_birthday":   "Aniversário",
		"app.edit_event":     "Editar evento",
		"app.edit_birthday":  "Editar aniversário",
		"app.empty_day":      "Nada neste dia.",
		"app.all_day":        "Dia inteiro",
		"app.title":          "Título",
		"app.notes":          "Notas",
		"app.starts":         "Começa",
		"app.ends":           "Termina",
		"app.start_time":     "Início",
		"app.end_time":       "Fim",
		"app.name":           "Nome",
		"app.month":          "Mês",
		"app.day":            "Dia",
		"app.year":           "Ano",
		"app.year_hint":      "Opcional. Serve para mostrar a idade.",
		"app.emoji":          "Emoji",
		"app.repeat":         "Repetir",
		"app.repeat_none":    "Nunca",
		"app.repeat_daily":   "Diário",
		"app.repeat_weekly":  "Semanal",
		"app.repeat_monthly": "Mensal",
		"app.repeat_yearly":  "Anual",
		"app.repeat_until":   "Até",
		"app.repeat_hint":    "Em branco repete para sempre. Editar muda todas as ocorrências.",
		"app.holidays":       "Feriados",
		"app.holidays_lede":  "Escolha os países cujos feriados você quer no mês.",
		"app.export":         "Exportar",
		"app.import":         "Importar",
		"app.import_done":    "Importados %{count} itens.",
		"app.import_invalid": "Esse arquivo não é um export válido do TansuCalendar.",
		"app.offline":        "Você está offline. Pode abrir meses que já viu, mas não salvar.",
		"app.overflow":       "+%{count}",

		"countries.BR": "Brasil",
		"countries.US": "Estados Unidos",
		"countries.SI": "Eslovênia",
		"countries.CZ": "Chéquia",

		"holidays.br.new_year":        "Confraternização Universal",
		"holidays.br.carnival":        "Carnaval",
		"holidays.br.good_friday":     "Sexta-feira Santa",
		"holidays.br.easter":          "Páscoa",
		"holidays.br.tiradentes":      "Tiradentes",
		"holidays.br.labor":           "Dia do Trabalho",
		"holidays.br.corpus_christi":  "Corpus Christi",
		"holidays.br.independence":    "Independência",
		"holidays.br.aparecida":       "Nossa Senhora Aparecida",
		"holidays.br.finados":         "Finados",
		"holidays.br.republic":        "Proclamação da República",
		"holidays.br.black_awareness": "Consciência Negra",
		"holidays.br.christmas":       "Natal",

		"holidays.us.new_year":     "Ano Novo",
		"holidays.us.mlk":          "Martin Luther King Jr.",
		"holidays.us.presidents":   "Presidents' Day",
		"holidays.us.memorial":     "Memorial Day",
		"holidays.us.juneteenth":   "Juneteenth",
		"holidays.us.independence": "Independência dos EUA",
		"holidays.us.labor":        "Labor Day",
		"holidays.us.columbus":     "Columbus Day",
		"holidays.us.veterans":     "Veterans Day",
		"holidays.us.thanksgiving": "Ação de Graças",
		"holidays.us.christmas":    "Natal",

		"holidays.si.new_year":      "Ano Novo",
		"holidays.si.new_year_2":    "Ano Novo (2º dia)",
		"holidays.si.preseren":      "Dia de Prešeren",
		"holidays.si.easter":        "Páscoa",
		"holidays.si.easter_monday": "Segunda-feira de Páscoa",
		"holidays.si.uprising":      "Dia da Revolta contra a Ocupação",
		"holidays.si.labor":         "Dia do Trabalho",
		"holidays.si.labor_2":       "Dia do Trabalho (2º dia)",
		"holidays.si.pentecost":     "Pentecostes",
		"holidays.si.statehood":     "Dia da Estatalidade",
		"holidays.si.assumption":    "Assunção",
		"holidays.si.reformation":   "Dia da Reforma",
		"holidays.si.all_saints":    "Todos os Santos",
		"holidays.si.christmas":     "Natal",
		"holidays.si.independence":  "Independência e Unidade",

		"holidays.cz.new_year":        "Ano Novo",
		"holidays.cz.good_friday":     "Sexta-feira Santa",
		"holidays.cz.easter_monday":   "Segunda-feira de Páscoa",
		"holidays.cz.labor":           "Dia do Trabalho",
		"holidays.cz.liberation":      "Dia da Libertação",
		"holidays.cz.cyril_methodius": "Cirilo e Metódio",
		"holidays.cz.jan_hus":         "Jan Hus",
		"holidays.cz.statehood":       "Dia da Estatalidade Tcheca",
		"holidays.cz.czechoslovak":    "Estado Checoslovaco Independente",
		"holidays.cz.freedom":         "Luta pela Liberdade e Democracia",
		"holidays.cz.christmas_eve":   "Véspera de Natal",
		"holidays.cz.christmas":       "Natal",
		"holidays.cz.st_stephen":      "Santo Estêvão",

		"errors.event.title.blank":               "Título não pode ficar em branco",
		"errors.event.title.too_long":            "Título é muito longo (máximo 200 caracteres)",
		"errors.event.body.too_long":             "Notas são muito longas (máximo 8000 caracteres)",
		"errors.event.starts_on.blank":           "Início não pode ficar em branco",
		"errors.event.ends_on.blank":             "Término não pode ficar em branco",
		"errors.event.ends_on.before_start":      "Término não pode ser antes do início",
		"errors.event.ends_on.too_long":          "Término não pode durar mais de um ano",
		"errors.event.starts_at.blank":           "Horário inicial não pode ficar em branco",
		"errors.event.ends_at.before_start":      "Horário final não pode ser antes do horário inicial",
		"errors.event.base.too_many":             "Dois mil eventos bastam.",
		"errors.event.emoji.too_long":            "Emoji é muito longo",
		"errors.event.repeat.invalid":            "Repetição inválida",
		"errors.event.repeat_until.invalid":      "Data final inválida",
		"errors.event.repeat_until.before_start": "Data final não pode ser antes do início",

		"errors.birthday.name.blank":     "Nome não pode ficar em branco",
		"errors.birthday.name.too_long":  "Nome é muito longo (máximo 200 caracteres)",
		"errors.birthday.body.too_long":  "Notas são muito longas (máximo 8000 caracteres)",
		"errors.birthday.month.range":    "Mês deve estar entre 1 e 12",
		"errors.birthday.day.range":      "Dia deve estar entre 1 e 31",
		"errors.birthday.day.invalid":    "Dia não é um dia real nesse mês",
		"errors.birthday.year.range":     "Ano deve estar entre 1900 e 2100",
		"errors.birthday.base.too_many":  "Quinhentos aniversários bastam.",
		"errors.birthday.emoji.too_long": "Emoji é muito longo",

		"js.invalid_credentials":         "Email ou senha inválidos.",
		"js.wrong_password":              "Senha incorreta.",
		"js.theme_system":                "Tema: sistema",
		"js.theme_light":                 "Tema: claro",
		"js.theme_dark":                  "Tema: escuro",
		"js.theme_switch":                "clique para alternar",
		"js.delete_event_confirm":        "Apagar este evento?",
		"js.delete_event_series_confirm": "Apagar este evento e todas as repetições?",
		"js.delete_birthday_confirm":     "Apagar este aniversário?",
	},
}
