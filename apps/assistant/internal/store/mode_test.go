package store

import "testing"

func TestConversationModeDefaultsAndHidesAnonymous(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "mode@x.com")
	c, err := s.CreateConversation(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeAssistant {
		t.Fatalf("mode = %q", c.Mode)
	}
	if _, err := s.CreateUserMessage(c.ID, "hi", false, false); err != nil {
		t.Fatal(err)
	}
	chat, err := s.OpenDraftForMode(u.ID, ModeChat)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Mode != ModeChat || chat.ID == c.ID {
		t.Fatalf("chat draft = %+v", chat)
	}
	again, err := s.OpenDraftForMode(u.ID, ModeAnonymous)
	if err != nil {
		t.Fatal(err)
	}
	if again.Mode != ModeAssistant {
		t.Fatalf("anonymous must not be stored, got %q", again.Mode)
	}
	if _, err := s.DB().Exec(`UPDATE conversations SET mode = ? WHERE id = ?`, ModeAnonymous, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindConversation(u.ID, c.ID); err != ErrNotFound {
		t.Fatalf("anonymous row visible to the account: %v", err)
	}
	list, err := s.ListConversations(u.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range list {
		if row.ID == c.ID || row.Mode == ModeAnonymous {
			t.Fatalf("listed anonymous thread: %+v", row)
		}
	}
	if got, err := s.GetConversation(c.ID); err != nil || got.Mode != ModeAnonymous {
		t.Fatalf("internal load = %+v, %v", got, err)
	}
}
