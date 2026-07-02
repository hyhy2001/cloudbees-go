package cred

import "testing"

func TestSetXMLElement_AttributeAwareReplace(t *testing.T) {
	xml := `<cred><secret plugin="foo">old</secret></cred>`
	got := setXMLElement(xml, "secret", "new")
	want := `<cred><secret plugin="foo">new</secret></cred>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetXMLElement_InsertsBeforeRootClose(t *testing.T) {
	xml := `<cred><id>x</id></cred>`
	got := setXMLElement(xml, "description", "hello")
	want := "<cred><id>x</id>\n  <description>hello</description></cred>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestValidateStore(t *testing.T) {
	if err := validateStore("system"); err != nil {
		t.Errorf("unexpected error for 'system': %v", err)
	}
	if err := validateStore("bogus"); err == nil {
		t.Error("expected error for invalid store")
	}
}

func TestValidateScope(t *testing.T) {
	if err := validateScope("GLOBAL"); err != nil {
		t.Errorf("unexpected error for 'GLOBAL': %v", err)
	}
	if err := validateScope("bogus"); err == nil {
		t.Error("expected error for invalid scope")
	}
}
