package config

import "testing"

func TestValidateRequiresFromWhenMailIsOn(t *testing.T) {
	if err := (Config{}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{ResendAPIKey: "re_test", ResendFrom: "tansu@example.com"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{ResendAPIKey: "re_test"}).Validate(); err == nil {
		t.Fatal("expected RESEND_FROM to be required")
	}
	if err := (Config{ResendAPIKey: "re_test", ResendFrom: "not-an-email"}).Validate(); err == nil {
		t.Fatal("expected a malformed sender to fail at boot")
	}
	if err := (Config{ResendAPIKey: "re_test", ResendFrom: "Tansu <tansu@example.com>"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if (Config{}).MailEnabled() {
		t.Fatal("empty key should leave mail off")
	}
	if !(Config{ResendAPIKey: "re_test"}).MailEnabled() {
		t.Fatal("key should enable mail")
	}
}
