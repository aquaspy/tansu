// Package i18n holds the en/pt string tables ported from
// config/locales. Keys use the same dot paths as the Rails app.
package i18n

import (
	"strconv"
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

var monthNames = map[Locale][]string{
	EN: {"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	PT: {"", "janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"},
}

var dayNames = map[Locale][]string{
	EN: {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	PT: {"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"},
}

// MonthName is the full month name, 1-12.
func MonthName(l Locale, month int) string {
	names := monthNames[l]
	if month < 1 || month > 12 {
		return ""
	}
	return names[month]
}

// MonthTitle mirrors date.formats.month: "August 2026" / "agosto de 2026".
func MonthTitle(l Locale, t time.Time) string {
	if l == PT {
		return MonthName(l, int(t.Month())) + " de " + t.Format("2006")
	}
	return MonthName(l, int(t.Month())) + " " + t.Format("2006")
}

// Ago renders a terse relative time for the live-quote status line:
// "just now" / "5m ago" / "3h ago" / "2d ago".
func Ago(l Locale, t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		if l == PT {
			return "agora mesmo"
		}
		return "just now"
	case d < time.Hour:
		n := strconv.Itoa(int(d.Minutes()))
		if l == PT {
			return "há " + n + "min"
		}
		return n + "m ago"
	case d < 48*time.Hour:
		n := strconv.Itoa(int(d.Hours()))
		if l == PT {
			return "há " + n + "h"
		}
		return n + "h ago"
	default:
		n := strconv.Itoa(int(d.Hours()) / 24)
		if l == PT {
			return "há " + n + " dias"
		}
		return n + "d ago"
	}
}

// DayLabel mirrors date.formats.day: "Tuesday, August 12" /
// "terça-feira, 12 de agosto".
func DayLabel(l Locale, t time.Time) string {
	day := dayNames[l][int(t.Weekday())]
	if l == PT {
		return day + ", " + t.Format("2") + " de " + MonthName(l, int(t.Month()))
	}
	return day + ", " + MonthName(l, int(t.Month())) + " " + t.Format("2")
}

var strings_ = map[Locale]map[string]string{
	EN: {
		"pwa.description": "A quiet personal spend tracker as a PWA.",

		"titles.app":      "TansuSpend",
		"titles.login":    "Sign in · TansuSpend",
		"titles.signup":   "Create account · TansuSpend",
		"titles.password": "Change password · TansuSpend",
		"titles.lock":     "Unlock · TansuSpend",
		"titles.tokens":   "API tokens · TansuSpend",
		"titles.agent":    "Link Assistant · TansuSpend",

		"api.invalid_month": "Month must use YYYY-MM.",

		"tokens.title":            "API tokens",
		"tokens.lede":             "Tokens let AI agents and other apps manage your spend through the API. A token has full access to your expenses, subscriptions, and payment days, and keeps working while the app is locked. Treat it like a password.",
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
		"auth.sign_in_lede":         "Your money lives on this server. Sign in to open it.",
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

		"app.unlock":                  "Unlock",
		"app.unlock_lede":             "Signed in as %{email}. Enter your password to show your money.",
		"app.lock":                    "Lock",
		"app.auto_lock_on":            "Turn on auto lock",
		"app.auto_lock_off":           "Turn off auto lock",
		"app.sign_out":                "Sign out",
		"app.cancel":                  "Cancel",
		"app.delete":                  "Delete",
		"app.save":                    "Save",
		"app.more":                    "More",
		"app.account":                 "Tansu Account",
		"app.account_label":           "Open Tansu Account",
		"app.today":                   "This month",
		"app.previous_month":          "Previous month",
		"app.next_month":              "Next month",
		"app.new_item":                "Add",
		"app.new_expense":             "Expense",
		"app.new_payment_day":         "Payment day",
		"app.new_subscription":        "Subscription",
		"app.edit_expense":            "Edit expense",
		"app.edit_payment_day":        "Edit payment day",
		"app.edit_subscription":       "Edit subscription",
		"app.leftover":                "Left this month",
		"app.leftover_negative":       "Over this month",
		"app.salary":                  "Salary",
		"app.subscriptions":           "Subscriptions",
		"app.payment_days":            "Payment days",
		"app.expenses":                "Spent",
		"app.daily":                   "Daily",
		"app.title":                   "Title",
		"app.amount":                  "Amount",
		"app.notes":                   "Notes",
		"app.currency":                "Currency",
		"app.category":                "Category",
		"app.category_none":           "No category",
		"app.date":                    "Date",
		"app.due_day":                 "Day of month",
		"app.due_on":                  "Due date",
		"app.day_n":                   "Day %{day}",
		"app.interval":                "Repeats",
		"app.interval_monthly":        "Every month",
		"app.interval_yearly":         "Once a year",
		"app.billing_month":           "Billing month",
		"app.payment_day_placeholder": "Water, electricity, card…",
		"app.payment_day_hint":        "Every month. Log the amount as an expense when you pay.",
		"app.due_today":               "Due today",
		"app.due_passed":              "Already passed this month",
		"app.due_on_day":              "Every month on day %{day}",
		"app.active":                  "Active",
		"app.settings":                "Salary & currencies",
		"app.settings_lede":           "Leftover is salary minus this month's subscriptions and daily spend, converted with the rates below.",
		"app.settings_saved":          "Saved.",
		"app.home_currency":           "Show totals in",
		"app.income_currency":         "Salary currency",
		"app.fx_lede":                 "Manual fallback, one unit in your home currency. With BRL home, live rates below win while fresh.",
		"app.fx_rate":                 "1 %{code}",
		"app.fx_auto":                 "Live: USD %{usd} · EUR %{eur} · updated %{ago}",
		"app.fx_auto_unavailable":     "Live rates unavailable — using the manual values above.",
		"app.fx_refresh":              "Refresh now",
		"app.fx_refreshed":            "Rates updated.",
		"app.fx_refreshed_partial":    "One rate updated; the other failed.",
		"app.fx_refresh_failed":       "Could not fetch live rates.",
		"app.missing_rates":           "Set a rate for %{codes} or those rows stay out of leftover.",
		"app.salary_hint":             "Set a salary in the menu to see what's left after subscriptions and spend.",
		"app.empty_expenses":          "No daily spend this month.",
		"app.empty_payment_days":      "No payment days yet. Water, electricity, card…",
		"app.empty_subscriptions":     "No subscriptions yet.",
		"app.export":                  "Export",
		"app.import":                  "Import",
		"app.import_done":             "Imported %{count} items.",
		"app.import_invalid":          "That file is not a valid TansuSpend export.",
		"app.offline":                 "You're offline. You can open months you've already viewed, but changes won't save.",
		"app.invalid_month":           "That month isn't valid.",
		"app.add_expense":             "Add expense",
		"app.add_payment_day":         "Add day",
		"app.add_subscription":        "Add subscription",
		"app.skipped":                 "needs a rate",
		"app.usual_amount":            "Usual amount",

		"currencies.BRL": "Brazilian real",
		"currencies.USD": "US dollar",
		"currencies.EUR": "Euro",

		"categories.food":      "Food",
		"categories.transport": "Transport",
		"categories.home":      "Home",
		"categories.health":    "Health",
		"categories.leisure":   "Leisure",
		"categories.other":     "Other",

		"errors.expense.title.blank":         "Title can't be blank.",
		"errors.expense.title.too_long":      "Title is too long (maximum is 200 characters).",
		"errors.expense.amount.greater_than": "Amount must be greater than 0.",
		"errors.expense.currency.invalid":    "Currency is not included in the list.",
		"errors.expense.spent_on.blank":      "Date can't be blank.",
		"errors.expense.spent_on.invalid":    "Date is not a valid date.",
		"errors.expense.category.invalid":    "Category is not included in the list.",
		"errors.expense.notes.too_long":      "Notes is too long (maximum is 4000 characters).",
		"errors.expense.base.too_many":       "Ten thousand expenses is enough.",

		"errors.subscription.title.blank":           "Title can't be blank.",
		"errors.subscription.title.too_long":        "Title is too long (maximum is 200 characters).",
		"errors.subscription.amount.greater_than":   "Amount must be greater than 0.",
		"errors.subscription.currency.invalid":      "Currency is not included in the list.",
		"errors.subscription.interval.invalid":      "Repeats is not included in the list.",
		"errors.subscription.due_day.invalid":       "Due day must be between 1 and 31.",
		"errors.subscription.billing_month.invalid": "Billing month must be between 1 and 12.",
		"errors.subscription.notes.too_long":        "Notes is too long (maximum is 4000 characters).",
		"errors.subscription.base.too_many":         "Two hundred subscriptions is enough.",

		"errors.payment_day.title.blank":     "Title can't be blank.",
		"errors.payment_day.title.too_long":  "Title is too long (maximum is 200 characters).",
		"errors.payment_day.due_day.blank":   "Day of month can't be blank.",
		"errors.payment_day.due_day.invalid": "Day of month must be between 1 and 31.",
		"errors.payment_day.notes.too_long":  "Notes is too long (maximum is 4000 characters).",
		"errors.payment_day.base.too_many":   "Two hundred payment days is enough.",

		"errors.user.fx.invalid":              "Exchange rate must be a positive number.",
		"errors.user.income.negative":         "Salary must be greater than or equal to 0.",
		"errors.user.home_currency.invalid":   "Home currency is not included in the list.",
		"errors.user.income_currency.invalid": "Salary currency is not included in the list.",

		"js.invalid_credentials":         "Invalid email or password.",
		"js.wrong_password":              "Wrong password.",
		"js.theme_system":                "Theme: system",
		"js.theme_light":                 "Theme: light",
		"js.theme_dark":                  "Theme: dark",
		"js.theme_switch":                "click to switch",
		"js.delete_expense_confirm":      "Delete this expense?",
		"js.delete_payment_day_confirm":  "Delete this payment day?",
		"js.delete_subscription_confirm": "Delete this subscription?",
	},
	PT: {
		"pwa.description": "Um controle pessoal de gastos calmo como PWA.",

		"titles.app":      "TansuSpend",
		"titles.login":    "Entrar · TansuSpend",
		"titles.signup":   "Criar conta · TansuSpend",
		"titles.password": "Mudar senha · TansuSpend",
		"titles.lock":     "Desbloquear · TansuSpend",
		"titles.tokens":   "Tokens de API · TansuSpend",
		"titles.agent":    "Ligar o Assistant · TansuSpend",

		"api.invalid_month": "O mês deve usar AAAA-MM.",

		"tokens.title":            "Tokens de API",
		"tokens.lede":             "Tokens permitem que agentes de IA e outros apps gerenciem seus gastos pela API. Um token tem acesso total às despesas, assinaturas e dias de pagamento e continua valendo com o app bloqueado. Trate como uma senha.",
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
		"auth.sign_in_lede":         "Seu dinheiro fica neste servidor. Entre para abri-lo.",
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

		"app.unlock":                  "Desbloquear",
		"app.unlock_lede":             "Conectado como %{email}. Digite a senha para ver seus gastos.",
		"app.lock":                    "Bloquear",
		"app.auto_lock_on":            "Ligar bloqueio automático",
		"app.auto_lock_off":           "Desligar bloqueio automático",
		"app.sign_out":                "Sair",
		"app.cancel":                  "Cancelar",
		"app.delete":                  "Apagar",
		"app.save":                    "Salvar",
		"app.more":                    "Mais",
		"app.account":                 "Tansu Account",
		"app.account_label":           "Abrir o Tansu Account",
		"app.today":                   "Este mês",
		"app.previous_month":          "Mês anterior",
		"app.next_month":              "Próximo mês",
		"app.new_item":                "Adicionar",
		"app.new_expense":             "Gasto",
		"app.new_payment_day":         "Dia de pagamento",
		"app.new_subscription":        "Assinatura",
		"app.edit_expense":            "Editar gasto",
		"app.edit_payment_day":        "Editar dia de pagamento",
		"app.edit_subscription":       "Editar assinatura",
		"app.leftover":                "Sobra neste mês",
		"app.leftover_negative":       "Estouro neste mês",
		"app.salary":                  "Salário",
		"app.subscriptions":           "Assinaturas",
		"app.payment_days":            "Dias de pagamento",
		"app.expenses":                "Gastos",
		"app.daily":                   "Do dia",
		"app.title":                   "Título",
		"app.amount":                  "Valor",
		"app.notes":                   "Notas",
		"app.currency":                "Moeda",
		"app.category":                "Categoria",
		"app.category_none":           "Sem categoria",
		"app.date":                    "Data",
		"app.due_day":                 "Dia do mês",
		"app.due_on":                  "Vencimento",
		"app.day_n":                   "Dia %{day}",
		"app.interval":                "Repete",
		"app.interval_monthly":        "Todo mês",
		"app.interval_yearly":         "Uma vez ao ano",
		"app.billing_month":           "Mês da cobrança",
		"app.payment_day_placeholder": "Água, luz, cartão…",
		"app.payment_day_hint":        "Todo mês, nesse dia. O valor entra como gasto quando você pagar.",
		"app.due_today":               "Vence hoje",
		"app.due_passed":              "Já passou neste mês",
		"app.due_on_day":              "Todo mês no dia %{day}",
		"app.active":                  "Ativa",
		"app.settings":                "Salário e moedas",
		"app.settings_lede":           "A sobra é o salário menos assinaturas e gastos do mês, convertidos com as cotações abaixo.",
		"app.settings_saved":          "Salvo.",
		"app.home_currency":           "Mostrar totais em",
		"app.income_currency":         "Moeda do salário",
		"app.fx_lede":                 "Reserva manual, uma unidade na moeda principal. Com real de base, a cotação ao vivo vale enquanto recente.",
		"app.fx_rate":                 "1 %{code}",
		"app.fx_auto":                 "Ao vivo: USD %{usd} · EUR %{eur} · atualizado %{ago}",
		"app.fx_auto_unavailable":     "Cotação ao vivo indisponível — valendo os manuais acima.",
		"app.fx_refresh":              "Atualizar agora",
		"app.fx_refreshed":            "Cotações atualizadas.",
		"app.fx_refreshed_partial":    "Uma cotação atualizada; a outra falhou.",
		"app.fx_refresh_failed":       "Não foi possível buscar as cotações.",
		"app.missing_rates":           "Defina a cotação de %{codes} ou essas linhas ficam fora da sobra.",
		"app.salary_hint":             "Coloque o salário no menu para ver o que sobra depois das assinaturas e gastos.",
		"app.empty_expenses":          "Nenhum gasto do dia neste mês.",
		"app.empty_payment_days":      "Nenhum dia de pagamento ainda. Água, luz, cartão…",
		"app.empty_subscriptions":     "Nenhuma assinatura ainda.",
		"app.export":                  "Exportar",
		"app.import":                  "Importar",
		"app.import_done":             "Importados %{count} itens.",
		"app.import_invalid":          "Esse arquivo não é um export válido do TansuSpend.",
		"app.offline":                 "Você está offline. Pode abrir meses que já viu, mas não salvar.",
		"app.invalid_month":           "Esse mês não é válido.",
		"app.add_expense":             "Adicionar gasto",
		"app.add_payment_day":         "Adicionar dia",
		"app.add_subscription":        "Adicionar assinatura",
		"app.skipped":                 "falta cotação",
		"app.usual_amount":            "Valor usual",

		"currencies.BRL": "Real brasileiro",
		"currencies.USD": "Dólar americano",
		"currencies.EUR": "Euro",

		"categories.food":      "Comida",
		"categories.transport": "Transporte",
		"categories.home":      "Casa",
		"categories.health":    "Saúde",
		"categories.leisure":   "Lazer",
		"categories.other":     "Outros",

		"errors.expense.title.blank":         "Título não pode ficar em branco.",
		"errors.expense.title.too_long":      "Título é muito longo (máximo 200 caracteres).",
		"errors.expense.amount.greater_than": "Valor precisa ser maior que 0.",
		"errors.expense.currency.invalid":    "Moeda não está incluída na lista.",
		"errors.expense.spent_on.blank":      "Data não pode ficar em branco.",
		"errors.expense.spent_on.invalid":    "Data não é uma data válida.",
		"errors.expense.category.invalid":    "Categoria não está incluída na lista.",
		"errors.expense.notes.too_long":      "Notas é muito longo (máximo 4000 caracteres).",
		"errors.expense.base.too_many":       "Dez mil gastos bastam.",

		"errors.subscription.title.blank":           "Título não pode ficar em branco.",
		"errors.subscription.title.too_long":        "Título é muito longo (máximo 200 caracteres).",
		"errors.subscription.amount.greater_than":   "Valor precisa ser maior que 0.",
		"errors.subscription.currency.invalid":      "Moeda não está incluída na lista.",
		"errors.subscription.interval.invalid":      "Repete não está incluído na lista.",
		"errors.subscription.due_day.invalid":       "Vencimento precisa estar entre 1 e 31.",
		"errors.subscription.billing_month.invalid": "Mês da cobrança precisa estar entre 1 e 12.",
		"errors.subscription.notes.too_long":        "Notas é muito longo (máximo 4000 caracteres).",
		"errors.subscription.base.too_many":         "Duzentas assinaturas bastam.",

		"errors.payment_day.title.blank":     "Título não pode ficar em branco.",
		"errors.payment_day.title.too_long":  "Título é muito longo (máximo 200 caracteres).",
		"errors.payment_day.due_day.blank":   "Dia do mês não pode ficar em branco.",
		"errors.payment_day.due_day.invalid": "Dia do mês precisa estar entre 1 e 31.",
		"errors.payment_day.notes.too_long":  "Notas é muito longo (máximo 4000 caracteres).",
		"errors.payment_day.base.too_many":   "Duzentos dias de pagamento bastam.",

		"errors.user.fx.invalid":              "Cotação precisa ser um número positivo.",
		"errors.user.income.negative":         "Salário deve ser maior ou igual a 0.",
		"errors.user.home_currency.invalid":   "Moeda principal não está incluída na lista.",
		"errors.user.income_currency.invalid": "Moeda do salário não está incluída na lista.",

		"js.invalid_credentials":         "Email ou senha inválidos.",
		"js.wrong_password":              "Senha incorreta.",
		"js.theme_system":                "Tema: sistema",
		"js.theme_light":                 "Tema: claro",
		"js.theme_dark":                  "Tema: escuro",
		"js.theme_switch":                "clique para alternar",
		"js.delete_expense_confirm":      "Apagar este gasto?",
		"js.delete_payment_day_confirm":  "Apagar este dia de pagamento?",
		"js.delete_subscription_confirm": "Apagar esta assinatura?",
	},
}
