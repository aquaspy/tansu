package store

import "testing"

func TestOpenDraftInheritsPersonality(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "inherit@x.com")
	first, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Mode != ModeAssistant {
		t.Fatalf("first draft = %q", first.Mode)
	}
	if _, err := s.CreateUserMessage(first.ID, "hi", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE conversations SET mode = ? WHERE id = ?`, ModeChat, first.ID); err != nil {
		t.Fatal(err)
	}
	next, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == first.ID || next.Mode != ModeChat {
		t.Fatalf("new draft = %+v, want a new Conversa chat", next)
	}
	if err := s.SetPreferredPersonality(u.ID, ModeAssistant); err != nil {
		t.Fatal(err)
	}
	again, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Mode != ModeAssistant {
		t.Fatalf("preference = %q, want assistant", again.Mode)
	}
}

func TestSetDraftModeOnlyWhenEmpty(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "draft@x.com")
	blank, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDraftMode(u.ID, blank.ID, ModeChat); err != nil {
		t.Fatal(err)
	}
	got, err := s.FindConversation(u.ID, blank.ID)
	if err != nil || got.Mode != ModeChat {
		t.Fatalf("blank mode = %+v err %v", got, err)
	}
	if _, err := s.CreateUserMessage(blank.ID, "kept", false, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDraftMode(u.ID, blank.ID, ModeAssistant); err != ErrNotFound {
		t.Fatalf("filled draft err = %v", err)
	}
	got, _ = s.FindConversation(u.ID, blank.ID)
	if got.Mode != ModeChat {
		t.Fatalf("filled mode changed to %q", got.Mode)
	}
}

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
