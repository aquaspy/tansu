package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aquasp/kurachat/internal/i18n"
)

// Toggling web search must only rewrite the tail of the model input: the
// shared prefix (base prompt, date, summary, history) stays byte-identical
// so the provider cache keeps hitting.
func TestSearchToggleKeepsPrefixStable(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hello", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	svc := testService(t, st, &fakeLLM{})

	off, _, err := svc.windowedMessages(conv, asst, i18n.EN, false)
	if err != nil {
		t.Fatal(err)
	}
	on, _, err := svc.windowedMessages(conv, asst, i18n.EN, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != len(on) {
		t.Fatalf("lengths differ: off=%d on=%d", len(off), len(on))
	}
	for i := 0; i < len(off)-1; i++ {
		a, _ := json.Marshal(off[i])
		b, _ := json.Marshal(on[i])
		if string(a) != string(b) {
			t.Fatalf("prefix diverged at message %d:\n%s\n%s", i, a, b)
		}
	}
	last := func(input []any) string {
		m, _ := input[len(input)-1].(map[string]any)
		if m == nil || m["role"] != "system" {
			return ""
		}
		s, _ := m["content"].(string)
		return s
	}
	if tail := last(off); !strings.Contains(tail, "cannot browse") {
		t.Fatalf("plain tail = %q", tail)
	}
	if tail := last(on); !strings.Contains(tail, "fresh web search") {
		t.Fatalf("search tail = %q", tail)
	}
}

// The dropped-images notice for text-only models also rides at the end,
// after the per-turn search/plain note.
func TestVisionNoticeIsTrailing(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	um, _ := st.CreateUserMessage(conv.ID, "Look", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	svc := testService(t, st, &fakeLLM{})
	storeImage(t, svc, um.ID)
	svc.Config.Model = "txt/model"
	svc.Catalog = catalogStub(t)

	input, _, err := svc.windowedMessages(conv, asst, i18n.EN, false)
	if err != nil {
		t.Fatal(err)
	}
	text := func(item any) string {
		m, _ := item.(map[string]any)
		if m == nil || m["role"] != "system" {
			return ""
		}
		s, _ := m["content"].(string)
		return s
	}
	if n := len(input); n < 2 ||
		!strings.Contains(text(input[n-1]), "can't see them") ||
		!strings.Contains(text(input[n-2]), "cannot browse") {
		raw, _ := json.Marshal(input)
		t.Fatalf("tail = %s", raw)
	}
}
