// Package i18n holds the en/pt string tables ported from
// config/locales. Keys use the same dot paths as the Rails app.
package i18n

import "strings"

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

// T looks up key ("chat.thinking") and interpolates %{name} pairs.
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

var strings_ = map[Locale]map[string]string{
	EN: {
		"pwa.description": "Private self-hosted chat as a PWA.",

		"titles.app":      "Tansu Assistant",
		"titles.login":    "Sign in · Tansu Assistant",
		"titles.signup":   "Create account · Tansu Assistant",
		"titles.password": "Change password · Tansu Assistant",
		"titles.lock":     "Unlock · Tansu Assistant",
		"titles.shared":   "%{title} · Tansu Assistant",

		"auth.sign_in":              "Sign in",
		"auth.sign_in_lede":         "Your chats live on this server. Sign in to open them.",
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
		"app.unlock_lede":   "Signed in as %{email}. Enter your password to show the chats.",
		"app.lock":          "Lock",
		"app.auto_lock_on":  "Turn on auto lock",
		"app.auto_lock_off": "Turn off auto lock",
		"app.sign_out":      "Sign out",
		"app.cancel":        "Cancel",
		"app.close":         "Close",
		"app.delete":        "Delete",
		"app.more":          "More",
		"app.account":       "Tansu Account",
		"app.account_label": "Open Tansu Account",
		"app.share":         "Share",
		"app.share_lede":    "Anyone with the link can read this chat. They don't need an account.",
		"app.share_create":  "Create link",
		"app.share_new":     "New link",
		"app.share_stop":    "Remove link",
		"app.copy":          "Copy",
		"app.copied":        "Copied",
		"app.shared_from":   "Shared from Tansu Assistant",
		"app.search":        "Search",

		"chat.new":                "New chat",
		"chat.empty":              "Pick a chat or start a new one.",
		"chat.empty_list":         "No chats yet.",
		"chat.placeholder":        "Message…",
		"chat.placeholder_anon":   "Anonymous message…",
		"chat.send":               "Send",
		"chat.attach":             "Attach files",
		"chat.voice_mic":          "Talk",
		"chat.voice_stop":         "Stop",
		"chat.voice":              "Voice",
		"chat.voice_read":         "Read aloud",
		"chat.voice_read_tip":     "Speak every reply automatically",
		"chat.voice_send":         "Auto-send",
		"chat.voice_send_tip":     "Send dictated messages right away",
		"chat.tier_cheap":         "Cheap",
		"chat.tier_medium":        "Medium",
		"chat.tier_expensive":     "Expensive",
		"chat.polish_prompt":      "You clean up dictated transcripts. Fix grammar, punctuation, and capitalization; organize into sentences and short paragraphs. Keep the user's language and exact meaning. Output only the cleaned text, no commentary.",
		"chat.speak":              "Listen",
		"chat.stop_speech":        "Stop",
		"chat.image_prompt":       "What's in this image?",
		"chat.doc_prompt":         "Summarize this document.",
		"chat.no_vision_note":     "Note: the user attached %{n} image(s) in this conversation, but your model cannot process images. Say briefly that you can't see them and suggest switching to a vision-capable model.",
		"chat.bad_file":           "Those files can't be used. Images (JPEG, PNG, WebP, GIF), PDF, Word (.docx) or PowerPoint (.pptx), up to 8 MB each, max 4.",
		"chat.sources":            "Sources",
		"chat.thinking":           "Thinking…",
		"chat.retrying":           "Retrying…",
		"chat.offline":            "You're offline. You can read, but not send.",
		"chat.in_flight":          "Wait for the current reply to finish.",
		"chat.too_many":           "Too many messages. Wait a moment.",
		"chat.blank":              "Write something first.",
		"chat.cannot_retry":       "That reply cannot be retried.",
		"chat.failed":             "The reply failed.",
		"chat.retry":              "Retry",
		"chat.deleted":            "Chat deleted.",
		"chat.deleted_all":        "All chats deleted.",
		"chat.delete_all":         "Delete all chats",
		"chat.delete_all_confirm": "Delete every chat on this account? This cannot be undone.",
		"chat.rename":             "Rename",
		"chat.delete_confirm":     "Delete this chat?",
		"chat.untitled":           "Untitled",
		"chat.cost":               "%{amount}",
		"chat.cost_est":           "est. %{amount}",
		"chat.cost_hint":          "Amount billed by OpenRouter for this chat (model, reasoning, cache, and web search).",
		"chat.cost_hint_est":      "Some turns lack the API's billed total, so this sum is incomplete.",
		"chat.model":              "Model",
		"chat.effort":             "Effort",
		"chat.web_search":         "Web search",
		"chat.deep_search":        "Deep search",
		"chat.web":                "Web",
		"chat.deep":               "Deep",
		"chat.searched":           "web",
		"chat.searched_deep":      "deep",
		"chat.system_prompt": "You are Tansu Assistant, a private assistant on the user's server.\n" +
			"Be clear and useful. Short answers for simple questions; thorough for research, comparisons, numbers, or current facts.\n" +
			"Reply in the user's language unless they write in another.\n" +
			"In Assistente mode you can read and change People, Spend, Calendar, and Notes only through tools, and only when a status note says that app is connected. If it is not, say so and suggest reconnecting or retrying. Never claim a change succeeded unless a tool result in this turn says so.\n",
		"chat.system_prompt_chat": "You are Tansu Assistant in Conversa mode, a private chat on the user's server.\n" +
			"Be clear and useful. Short answers for simple questions; thorough when the question needs it.\n" +
			"Reply in the user's language unless they write in another.\n" +
			"You cannot read or change People, Spend, Calendar, or Notes. If asked, say this chat does not use those apps and that Assistente mode does. Never claim you saved or changed anything in those apps.\n",
		"chat.system_prompt_anon": "You are Tansu Assistant in anonymous mode. This conversation is not saved to the user's account.\n" +
			"Be clear and useful. Short answers for simple questions; thorough when the question needs it.\n" +
			"Reply in the user's language unless they write in another.\n" +
			"You cannot read or change People, Spend, Calendar, or Notes, and you must not claim this chat was saved to their account. If asked to use those apps, say this mode cannot and that Assistente mode can.\n",
		"chat.health_intro":        "App connections right now. Trust this list over the conversation. Only a connected app can be read or changed.",
		"chat.health_ok":           "connected",
		"chat.health_auth":         "unavailable (sign-in failed: %{detail}). Tell the user to reconnect it under Connect apps. Do not invent a successful action.",
		"chat.health_down":         "unavailable (%{detail}). Tell the user it did not respond and suggest retrying. Do not invent a successful action.",
		"chat.health_off":          "not linked. If asked to use it, say so and suggest Connect apps. Do not invent a successful action.",
		"chat.health_broken":       "link cannot be opened. Tell the user to link it again under Connect apps. Do not invent a successful action.",
		"chat.health_unconfigured": "not set up on this server. Say so if asked. Do not invent a successful action.",
		"chat.health_rule":         "Never say you saved, changed, or deleted something in an app that is not connected.",
		"chat.badge_ok":            "%{app} connected",
		"chat.badge_down":          "%{app} unavailable",
		"mode.label":               "Chat mode",
		"mode.assistant":           "Assistente",
		"mode.chat":                "Conversa",
		"mode.anonymous":           "Anônimo",
		"mode.assistant_tip":       "Uses linked apps: People, Spend, Calendar, and Notes. Saved to your account.",
		"mode.chat_tip":            "Plain chat. Does not change your other apps. Saved to your account.",
		"mode.anonymous_tip":       "Not saved to your account. The text still goes to the model. No apps.",
		"mode.not_saved":           "Not saved to your account",
		"mode.not_saved_detail":    "This chat stays in this browser. What you send still goes to the model.",
		"mode.clear":               "Clear now",
		"mode.clear_on_close":      "Clear on close",
		"mode.clear_on_close_tip":  "Drop this chat when the tab closes",
		"mode.hero_assistant":      "How can I help?",
		"mode.hero_chat":           "What should we talk about?",
		"mode.hero_anonymous":      "Anonymous chat",
		"mode.assistant_lede":      "I can read and change People, Spend, Calendar, and Notes for you.",
		"mode.chat_lede":           "Plain chat, saved to your account. It doesn't touch your other apps.",
		"mode.anonymous_lede":      "Stays in this browser only, out of your account history.",
		"mode.new_in":              "New chat in",
		"mode.local_menu":          "Chats in this browser",
		"chat.examples_hint":       "Tap a prompt to fill the box. Nothing is sent until you do.",
		"chat.what_can_i_do":       "What can I do?",
		"chat.more_examples":       "More examples",
		"chat.apps_ok":             "Apps connected",
		"chat.apps_down_many":      "%{n} apps unavailable",
		"chat.apps_none":           "No apps connected · Connect apps",
		"chat.system_note_plain":   "You cannot browse the web. Do not claim you searched.",
		"chat.confirm_delete":      "Delete",
		"chat.confirm_send":        "Send",
		"chat.action_cancelled":    "Action cancelled.",
		"chat.action_unknown":      "The app did not answer. Check before trying again.",
		"chat.action_failed":       "That did not save.",
		"chat.action_reconnect":    "That app needs to be linked again. Open Connect apps.",
		"apps.title":               "Apps",
		"apps.entry":               "Connect apps",
		"apps.lede":                "Link a sibling app and this chat can read and change it. Text that is read is sent to the model. The token stays encrypted on this server and keeps working while that app is locked.",
		"apps.notes":               "Notes",
		"apps.calendar":            "Calendar",
		"apps.spend":               "Spend",
		"apps.people":              "People",
		"apps.email":               "Email",
		"apps.connect":             "Link",
		"apps.disconnect":          "Unlink",
		"apps.linked":              "Linked as %{email}",
		"apps.unconfigured":        "Not configured on this server",
		"apps.nokey":               "This server has no apps key",
		"apps.broken":              "Link again — the saved token cannot be opened",
		"apps.back":                "Back to chat",
		"apps.bad_return":          "Could not finish linking.",
		"apps.mismatch":            "That account is not this Assistant account.",
		"apps.disconnected":        "Unlinked.",
		"apps.revoke_remote":       "Unlinked here. The other app was offline — revoke the token named Tansu Assistant under More → API tokens.",
		"titles.apps":              "Apps · Tansu Assistant",
		"chat.system_note_search":  "This turn includes fresh web search results. Ground current facts in them and cite sources with markdown links. Do not claim browsing beyond the provided results.",

		"js.invalid_credentials": "Invalid email or password.",
		"js.wrong_password":      "Wrong password.",
		"js.untitled":            "Untitled",
		"js.empty_list":          "No chats here.",
		"js.delete_confirm":      "Delete this chat?",
		"js.theme_system":        "Theme: system",
		"js.theme_light":         "Theme: light",
		"js.theme_dark":          "Theme: dark",
		"js.theme_switch":        "click to switch",
		"js.copied":              "Copied",
		"js.voice_failed":        "Voice failed — try again.",
		"js.voice_empty":         "Didn't catch that — try again.",
		"js.voice_denied":        "Microphone blocked by the browser.",
		"js.voice_transcribing":  "Transcribing…",
		"js.voice_polishing":     "Organizing…",
		"js.anon_failed":         "The reply failed.",
	},
	PT: {
		"pwa.description": "Chat privado self-hosted como PWA.",

		"titles.app":      "Tansu Assistant",
		"titles.login":    "Entrar · Tansu Assistant",
		"titles.signup":   "Criar conta · Tansu Assistant",
		"titles.password": "Mudar senha · Tansu Assistant",
		"titles.lock":     "Desbloquear · Tansu Assistant",
		"titles.shared":   "%{title} · Tansu Assistant",

		"auth.sign_in":              "Entrar",
		"auth.sign_in_lede":         "Os chats ficam neste servidor. Entre para abri-los.",
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
		"app.unlock_lede":   "Conectado como %{email}. Digite a senha para ver os chats.",
		"app.lock":          "Bloquear",
		"app.auto_lock_on":  "Ligar bloqueio automático",
		"app.auto_lock_off": "Desligar bloqueio automático",
		"app.sign_out":      "Sair",
		"app.cancel":        "Cancelar",
		"app.close":         "Fechar",
		"app.delete":        "Apagar",
		"app.more":          "Mais",
		"app.account":       "Tansu Account",
		"app.account_label": "Abrir o Tansu Account",
		"app.share":         "Compartilhar",
		"app.share_lede":    "Quem tiver o link lê o chat. Não precisa de conta.",
		"app.share_create":  "Criar link",
		"app.share_new":     "Novo link",
		"app.share_stop":    "Remover link",
		"app.copy":          "Copiar",
		"app.copied":        "Copiado",
		"app.shared_from":   "Compartilhado pelo Tansu Assistant",
		"app.search":        "Buscar",

		"chat.new":                "Novo chat",
		"chat.empty":              "Escolha um chat ou comece um novo.",
		"chat.empty_list":         "Nenhum chat ainda.",
		"chat.placeholder":        "Mensagem…",
		"chat.placeholder_anon":   "Mensagem anônima…",
		"chat.send":               "Enviar",
		"chat.attach":             "Anexar arquivos",
		"chat.voice_mic":          "Falar",
		"chat.voice_stop":         "Parar",
		"chat.voice":              "Voz",
		"chat.voice_read":         "Ler em voz alta",
		"chat.voice_read_tip":     "Fala todas as respostas sozinha",
		"chat.voice_send":         "Envio automático",
		"chat.voice_send_tip":     "Envia o ditado na hora",
		"chat.tier_cheap":         "Barato",
		"chat.tier_medium":        "Médio",
		"chat.tier_expensive":     "Caro",
		"chat.polish_prompt":      "Você organiza transcrições ditadas. Corrija gramática, pontuação e maiúsculas; organize em frases e parágrafos curtos. Mantenha o idioma e o sentido exato. Responda só com o texto limpo, sem comentários.",
		"chat.speak":              "Ouvir",
		"chat.stop_speech":        "Parar",
		"chat.image_prompt":       "O que tem nesta imagem?",
		"chat.doc_prompt":         "Resuma este documento.",
		"chat.no_vision_note":     "Nota: o usuário anexou %{n} imagem(ns) nesta conversa, mas seu modelo não processa imagens. Diga brevemente que não consegue vê-las e sugira trocar para um modelo com visão.",
		"chat.bad_file":           "Esses arquivos não servem. Imagens (JPEG, PNG, WebP, GIF), PDF, Word (.docx) ou PowerPoint (.pptx), até 8 MB cada, no máximo 4.",
		"chat.sources":            "Fontes",
		"chat.thinking":           "Pensando…",
		"chat.retrying":           "Tentando de novo…",
		"chat.offline":            "Você está offline. Pode ler, mas não enviar.",
		"chat.in_flight":          "Espere a resposta atual terminar.",
		"chat.too_many":           "Muitas mensagens. Espere um pouco.",
		"chat.blank":              "Escreva algo primeiro.",
		"chat.cannot_retry":       "Essa resposta não pode ser tentada de novo.",
		"chat.failed":             "A resposta falhou.",
		"chat.retry":              "Tentar de novo",
		"chat.deleted":            "Chat apagado.",
		"chat.deleted_all":        "Todos os chats apagados.",
		"chat.delete_all":         "Apagar todos os chats",
		"chat.delete_all_confirm": "Apagar todos os chats desta conta? Isso não tem volta.",
		"chat.rename":             "Renomear",
		"chat.delete_confirm":     "Apagar este chat?",
		"chat.untitled":           "Sem título",
		"chat.cost":               "%{amount}",
		"chat.cost_est":           "est. %{amount}",
		"chat.cost_hint":          "Valor cobrado pelo OpenRouter neste chat (modelo, reasoning, cache e busca na web).",
		"chat.model":              "Modelo",
		"chat.effort":             "Esforço",
		"chat.web_search":         "Busca na web",
		"chat.deep_search":        "Busca profunda",
		"chat.web":                "Web",
		"chat.deep":               "Deep",
		"chat.searched":           "web",
		"chat.searched_deep":      "deep",
		"chat.cost_hint_est":      "Alguns turnos não têm o total faturado da API, então esta soma está incompleta.",
		"chat.system_prompt": "Você é o Tansu Assistant, um assistente privado no servidor do usuário.\n" +
			"Seja claro e útil. Respostas curtas para perguntas simples; completo em pesquisa, comparações, números ou fatos atuais.\n" +
			"Responda no idioma do usuário, a menos que ele escreva em outro.\n" +
			"No modo Assistente você só lê e altera Pessoas, Gastos, Calendário e Notas pelas ferramentas, e só quando uma nota de status disser que o app está conectado. Se não estiver, diga isso e sugira reconectar ou tentar de novo. Nunca diga que uma alteração deu certo sem um resultado de ferramenta neste turno.\n",
		"chat.system_prompt_chat": "Você é o Tansu Assistant no modo Conversa, um chat privado no servidor do usuário.\n" +
			"Seja claro e útil. Respostas curtas para perguntas simples; completo quando a pergunta pedir.\n" +
			"Responda no idioma do usuário, a menos que ele escreva em outro.\n" +
			"Você não pode ler nem alterar Pessoas, Gastos, Calendário ou Notas. Se pedirem, diga que este chat não usa esses apps e que o modo Assistente usa. Nunca diga que salvou ou alterou algo neles.\n",
		"chat.system_prompt_anon": "Você é o Tansu Assistant no modo anônimo. Esta conversa não é salva na conta do usuário.\n" +
			"Seja claro e útil. Respostas curtas para perguntas simples; completo quando a pergunta pedir.\n" +
			"Responda no idioma do usuário, a menos que ele escreva em outro.\n" +
			"Você não pode ler nem alterar Pessoas, Gastos, Calendário ou Notas, e não deve dizer que este chat foi salvo na conta. Se pedirem esses apps, diga que este modo não pode e que o modo Assistente pode.\n",
		"chat.health_intro":        "Conexões dos apps agora. Confie nesta lista, não na conversa. Só um app conectado pode ser lido ou alterado.",
		"chat.health_ok":           "conectado",
		"chat.health_auth":         "indisponível (login falhou: %{detail}). Diga para reconectar em Conectar apps. Não invente uma ação bem-sucedida.",
		"chat.health_down":         "indisponível (%{detail}). Diga que não respondeu e sugira tentar de novo. Não invente uma ação bem-sucedida.",
		"chat.health_off":          "não ligado. Se pedirem para usá-lo, diga isso e sugira Conectar apps. Não invente uma ação bem-sucedida.",
		"chat.health_broken":       "o vínculo não abre. Diga para ligar de novo em Conectar apps. Não invente uma ação bem-sucedida.",
		"chat.health_unconfigured": "não configurado neste servidor. Diga isso se pedirem. Não invente uma ação bem-sucedida.",
		"chat.health_rule":         "Nunca diga que salvou, alterou ou apagou algo num app que não está conectado.",
		"chat.badge_ok":            "%{app} conectado",
		"chat.badge_down":          "%{app} indisponível",
		"mode.label":               "Modo do chat",
		"mode.assistant":           "Assistente",
		"mode.chat":                "Conversa",
		"mode.anonymous":           "Anônimo",
		"mode.assistant_tip":       "Usa os apps ligados: Pessoas, Gastos, Calendário e Notas. Salvo na conta.",
		"mode.chat_tip":            "Conversa livre. Não altera os outros apps. Salvo na conta.",
		"mode.anonymous_tip":       "Não entra no histórico da conta. O texto ainda segue para o modelo. Sem apps.",
		"mode.not_saved":           "Não salvo na conta",
		"mode.not_saved_detail":    "Este chat fica neste navegador. O que você envia ainda segue para o modelo.",
		"mode.clear":               "Apagar agora",
		"mode.clear_on_close":      "Apagar ao fechar",
		"mode.clear_on_close_tip":  "Apaga este chat quando a aba fechar",
		"mode.hero_assistant":      "Como posso ajudar?",
		"mode.hero_chat":           "Sobre o que vamos conversar?",
		"mode.hero_anonymous":      "Chat anônimo",
		"mode.assistant_lede":      "Leio e altero Pessoas, Gastos, Calendário e Notas por você.",
		"mode.chat_lede":           "Conversa livre, salva na sua conta. Não mexe nos seus outros apps.",
		"mode.anonymous_lede":      "Fica só neste navegador, fora do histórico da conta.",
		"mode.new_in":              "Novo chat em",
		"mode.local_menu":          "Chats neste navegador",
		"chat.examples_hint":       "Um toque preenche a caixa. Nada é enviado até você mandar.",
		"chat.what_can_i_do":       "O que posso fazer?",
		"chat.more_examples":       "Mais exemplos",
		"chat.apps_ok":             "Apps conectados",
		"chat.apps_down_many":      "%{n} apps indisponíveis",
		"chat.apps_none":           "Nenhum app conectado · Conectar apps",
		"chat.system_note_plain":   "Você não navega na web. Não diga que buscou.",
		"chat.confirm_delete":      "Apagar",
		"chat.confirm_send":        "Enviar",
		"chat.action_cancelled":    "Ação cancelada.",
		"chat.action_unknown":      "O app não respondeu. Confira antes de tentar de novo.",
		"chat.action_failed":       "Isso não foi salvo.",
		"chat.action_reconnect":    "Esse app precisa ser ligado de novo. Abra Conectar apps.",
		"apps.title":               "Apps",
		"apps.entry":               "Conectar apps",
		"apps.lede":                "Ligue um app irmão e este chat pode ler e alterar o que está nele. O texto lido segue para o modelo. O token fica cifrado neste servidor e continua valendo com aquele app trancado.",
		"apps.notes":               "Notas",
		"apps.calendar":            "Calendário",
		"apps.spend":               "Gastos",
		"apps.people":              "Pessoas",
		"apps.email":               "E-mail",
		"apps.connect":             "Ligar",
		"apps.disconnect":          "Desligar",
		"apps.linked":              "Ligado como %{email}",
		"apps.unconfigured":        "Não configurado neste servidor",
		"apps.nokey":               "Este servidor não tem a chave dos apps",
		"apps.broken":              "Ligue de novo — o token salvo não abre",
		"apps.back":                "Voltar ao chat",
		"apps.bad_return":          "Não foi possível terminar a ligação.",
		"apps.mismatch":            "Essa conta não é a deste Assistant.",
		"apps.disconnected":        "Desligado.",
		"apps.revoke_remote":       "Desligado aqui. O outro app estava fora — revogue o token chamado Tansu Assistant em Mais → Tokens de API.",
		"titles.apps":              "Apps · Tansu Assistant",
		"chat.system_note_search":  "Este turno inclui resultados frescos de busca na web. Baseie fatos atuais neles e cite as fontes com links markdown. Não diga que navegou além dos resultados fornecidos.",

		"js.invalid_credentials": "Email ou senha inválidos.",
		"js.wrong_password":      "Senha incorreta.",
		"js.untitled":            "Sem título",
		"js.empty_list":          "Nenhum chat aqui.",
		"js.delete_confirm":      "Apagar este chat?",
		"js.theme_system":        "Tema: sistema",
		"js.theme_light":         "Tema: claro",
		"js.theme_dark":          "Tema: escuro",
		"js.theme_switch":        "clique para alternar",
		"js.copied":              "Copiado",
		"js.voice_failed":        "A voz falhou — tente de novo.",
		"js.voice_empty":         "Não entendi — tente de novo.",
		"js.voice_denied":        "Microfone bloqueado pelo navegador.",
		"js.voice_transcribing":  "Transcrevendo…",
		"js.voice_polishing":     "Organizando…",
		"js.anon_failed":         "A resposta falhou.",
	},
}
