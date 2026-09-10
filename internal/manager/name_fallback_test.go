package manager

import (
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
	null "gopkg.in/volatiletech/null.v6"
)

func TestCampaignNameFallbackAllFormats(t *testing.T) {
	m := newTestManager()
	for _, format := range []string{"html", "richtext", "markdown", "plain", "visual"} {
		t.Run(format, func(t *testing.T) {
			rule := models.NameFallback{Enabled: true, Value: "Business Team", InvalidValues: []string{"N/A"}}
			c := &models.Campaign{ContentType: format, Body: `Dear {{ .Customer.Name }},`,
				Subject: `For {{ .Customer.Name }}`, AltBody: null.StringFrom(`Hello {{ .Customer.Name }}`),
				TemplateNameFallback: rule, NameFallback: rule}
			if format == "visual" {
				c.TemplateNameFallback.Value = "wrong linked rule"
			}
			if err := c.CompileTemplate(m.GenericTemplateFuncs()); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"", "   ", "n/a", "Alice", "Bob"} {
				customer := models.Customer{Name: name, Email: "recipient@example.invalid"}
				msg, err := m.NewCampaignMessage(c, customer)
				if err != nil {
					t.Fatal(err)
				}
				want := rule.Resolve(name)
				if !strings.Contains(string(msg.Body()), "Dear "+want+",") || msg.Subject() != "For "+want {
					t.Fatalf("name %q: subject %q body %q", name, msg.Subject(), msg.Body())
				}
				if format != "plain" && string(msg.AltBody()) != "Hello "+want {
					t.Fatalf("alt body %q", msg.AltBody())
				}
				if msg.Customer.Name != name || customer.Name != name {
					t.Fatal("render must not change recipient data")
				}
			}
		})
	}
}
