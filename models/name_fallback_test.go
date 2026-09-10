package models

import (
	"html/template"
	"strings"
	"testing"
)

func TestNameFallback(t *testing.T) {
	rule := NameFallback{Enabled: true, Value: "Sir or Madam", InvalidValues: []string{"N/A", "未知", "-"}}
	for _, tc := range []struct{ input, want string }{
		{"", "Sir or Madam"}, {" \t\n", "Sir or Madam"}, {" n/a ", "Sir or Madam"},
		{"未知", "Sir or Madam"}, {"-", "Sir or Madam"}, {" John Smith ", "John Smith"},
		{"Ana N/A", "Ana N/A"}, {"李明", "李明"},
	} {
		if got := rule.Resolve(tc.input); got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
	rule.Enabled = false
	if rule.Resolve(" N/A ") != " N/A " {
		t.Fatal("disabled rule changed the original name")
	}
	for _, invalid := range []NameFallback{
		{Enabled: true}, {Enabled: true, Value: " \t"}, {Value: strings.Repeat("x", 201)},
		{Value: "line\nbreak"}, {InvalidValues: make([]string, 51)}, {InvalidValues: []string{"line\nbreak"}},
	} {
		if invalid.Validate() == nil {
			t.Errorf("accepted invalid rule: %+v", invalid)
		}
	}
}

func TestTransactionalNameFallbackAndClone(t *testing.T) {
	tpl := Template{Type: TemplateTypeTx, Body: `Dear {{ .Customer.Name }}`, Subject: `For {{ .Customer.Name }}`,
		NameFallback: NameFallback{Enabled: true, Value: `<Team & Co>`, InvalidValues: []string{"N/A"}}}
	if err := tpl.Compile(template.FuncMap{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "N/A", "Alice"} {
		customer := Customer{Name: name}
		msg := TxMessage{AltBody: `Hello {{ .Customer.Name }}`}
		if err := msg.Render(customer, &tpl, nil); err != nil {
			t.Fatal(err)
		}
		want := tpl.NameFallback.Resolve(name)
		if string(msg.Body) != "Dear "+template.HTMLEscapeString(want) || msg.Subject != "For "+want || msg.AltBody != "Hello "+want {
			t.Fatalf("incorrect rendered message: %+v / %s", msg, msg.Body)
		}
		if customer.Name != name {
			t.Fatal("modified customer data")
		}
	}
	clone := tpl.Clone("copy", "")
	clone.NameFallback.InvalidValues[0] = "changed"
	if tpl.NameFallback.InvalidValues[0] != "N/A" || clone.NameFallback.Value != tpl.NameFallback.Value {
		t.Fatal("clone must own its rules")
	}
	var restored NameFallback
	if err := restored.Scan(tpl.NameFallback.ValueForDB()); err != nil || restored.Resolve("") != tpl.NameFallback.Value {
		t.Fatal("rule did not round-trip", err)
	}
}
