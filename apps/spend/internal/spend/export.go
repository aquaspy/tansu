package spend

import (
	"encoding/json"
	"time"

	"github.com/aquasp/kuraspend/internal/store"
)

type exportUser struct {
	HomeCurrency       string            `json:"home_currency"`
	MonthlyIncomeCents int64             `json:"monthly_income_cents"`
	IncomeCurrency     string            `json:"income_currency"`
	FX                 map[string]string `json:"fx"`
}

type exportSubscription struct {
	Title        string `json:"title"`
	AmountCents  int64  `json:"amount_cents"`
	Currency     string `json:"currency"`
	Interval     string `json:"interval"`
	DueDay       *int   `json:"due_day"`
	BillingMonth *int   `json:"billing_month"`
	Active       bool   `json:"active"`
	Notes        string `json:"notes"`
}

type exportPaymentDay struct {
	Title  string `json:"title"`
	DueDay int    `json:"due_day"`
	Active bool   `json:"active"`
	Notes  string `json:"notes"`
}

type exportExpense struct {
	Title       string `json:"title"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	SpentOn     string `json:"spent_on"`
	Category    string `json:"category"`
	Notes       string `json:"notes"`
}

type exportPayload struct {
	App           string               `json:"app"`
	ExportedAt    string               `json:"exported_at"`
	User          exportUser           `json:"user"`
	Subscriptions []exportSubscription `json:"subscriptions"`
	PaymentDays   []exportPaymentDay   `json:"payment_days"`
	Expenses      []exportExpense      `json:"expenses"`
}

// Export mirrors MonthsController#export. Field order matches the Rails
// as_export hashes so exports stay interchangeable.
func Export(st *store.Store, user *store.User) ([]byte, error) {
	subs, err := st.ListSubscriptions(user.ID, false)
	if err != nil {
		return nil, err
	}
	days, err := st.ListPaymentDays(user.ID)
	if err != nil {
		return nil, err
	}
	expenses, err := st.ExportExpenses(user.ID)
	if err != nil {
		return nil, err
	}
	payload := exportPayload{
		App:        "TansuSpend",
		ExportedAt: time.Now().Format(time.RFC3339),
		User: exportUser{
			HomeCurrency:       user.HomeCurrency,
			MonthlyIncomeCents: user.MonthlyIncomeCents,
			IncomeCurrency:     user.IncomeCurrency,
			FX:                 store.FXHash(user),
		},
		Subscriptions: make([]exportSubscription, 0, len(subs)),
		PaymentDays:   make([]exportPaymentDay, 0, len(days)),
		Expenses:      make([]exportExpense, 0, len(expenses)),
	}
	for _, su := range subs {
		payload.Subscriptions = append(payload.Subscriptions, exportSubscription{
			Title: su.Title, AmountCents: su.AmountCents, Currency: su.Currency,
			Interval: su.Interval, DueDay: su.DueDay, BillingMonth: su.BillingMonth,
			Active: su.Active, Notes: su.Notes,
		})
	}
	for _, d := range days {
		payload.PaymentDays = append(payload.PaymentDays, exportPaymentDay{
			Title: d.Title, DueDay: d.DueDay, Active: d.Active, Notes: d.Notes,
		})
	}
	for _, e := range expenses {
		payload.Expenses = append(payload.Expenses, exportExpense{
			Title: e.Title, AmountCents: e.AmountCents, Currency: e.Currency,
			SpentOn: e.SpentOn, Category: e.Category, Notes: e.Notes,
		})
	}
	return json.MarshalIndent(payload, "", "  ")
}
