package views

import (
	"encoding/json"

	"github.com/aquasp/kurachat/internal/i18n"
)

// ExampleGroup is one app's prompt chips. Text fills the composer; it is not sent.
type ExampleGroup struct {
	App   string
	Label string
	Items []string
}

// ExampleGroups matches the tools Assistente can actually call.
func ExampleGroups(l i18n.Locale) []ExampleGroup {
	people := []string{
		"Create Ana, sister, phone 11 99999-0000",
		"Update Ana's address to 10 Flower Street",
	}
	spend := []string{
		"Log lunch for R$ 42.50 today under food",
		"How much did I spend on transport this month?",
	}
	calendar := []string{
		"Schedule dentist tomorrow from 15:00 to 16:00",
		"What is on my calendar this week?",
	}
	notes := []string{
		"Create a note in the Trip folder with a packing list",
		"Find notes about the budget",
	}
	if l == i18n.PT {
		people = []string{
			"Cria a Ana, irmã, telefone 11 99999-0000",
			"Atualiza o endereço da Ana para Rua das Flores, 10",
		}
		spend = []string{
			"Registra um almoço de R$ 42,50 hoje em alimentação",
			"Quanto gastei em transporte neste mês?",
		}
		calendar = []string{
			"Marca dentista amanhã das 15:00 às 16:00",
			"O que tenho na agenda esta semana?",
		}
		notes = []string{
			"Cria uma nota na pasta Viagem com a lista de malas",
			"Busca notas sobre o orçamento",
		}
	}
	return []ExampleGroup{
		{App: "people", Label: i18n.T(l, "apps.people"), Items: people},
		{App: "spend", Label: i18n.T(l, "apps.spend"), Items: spend},
		{App: "calendar", Label: i18n.T(l, "apps.calendar"), Items: calendar},
		{App: "notes", Label: i18n.T(l, "apps.notes"), Items: notes},
	}
}

// AnonSeed is JSON for the no-JS anonymous post so the browser can keep it.
func AnonSeed(msgs []AnonMessage) string {
	type row struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		HTML    string `json:"html,omitempty"`
	}
	out := make([]row, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, row{Role: m.Role, Content: m.Content, HTML: m.HTML})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func threadMode(d ShellData) string {
	if d.Anonymous {
		return "anonymous"
	}
	if d.Current != nil && d.Current.Conv != nil && d.Current.Conv.Mode != "" {
		return d.Current.Conv.Mode
	}
	return "assistant"
}
