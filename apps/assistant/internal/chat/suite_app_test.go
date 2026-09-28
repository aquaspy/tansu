package chat

import "testing"

func TestSuiteAppEmail(t *testing.T) {
	if suiteApp("mail_send") != "email" || suiteApp("spend_list") != "spend" || suiteApp("notes_search") != "notes" {
		t.Fatalf("mail %s spend %s notes %s", suiteApp("mail_send"), suiteApp("spend_list"), suiteApp("notes_search"))
	}
	if suiteApp("other") != "" {
		t.Fatalf("unknown %s", suiteApp("other"))
	}
}
