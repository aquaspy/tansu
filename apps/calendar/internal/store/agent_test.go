package store

import (
	"testing"
	"time"
)

func TestDeleteStaleAgentGrantsRevokesToken(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	u, err := st.CreateUser("a@x.com", "digest")
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := st.MintAssistantToken(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAgentGrant(u.ID, DigestAgentCode("code"), "challenge", raw, -time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteStaleAgentGrants(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AuthenticateToken(raw); err == nil {
		t.Fatal("expired grant left the token usable")
	}
	if _, err := st.ConsumeAgentGrant(DigestAgentCode("code")); err == nil {
		t.Fatal("expired grant still present")
	}
	_, live, err := st.MintAssistantToken(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAgentGrant(u.ID, DigestAgentCode("fresh"), "challenge", live, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteStaleAgentGrants(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AuthenticateToken(live); err != nil {
		t.Fatal("fresh grant was revoked")
	}
}
