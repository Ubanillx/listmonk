from __future__ import annotations

import unittest

from listmonk_marketing.campaigns import (
    find_unsupported_template_fields,
    validate_campaign_body_vars,
)


class TemplateVarTests(unittest.TestCase):
    def test_upstream_subscriber_is_rejected(self) -> None:
        body = 'Hello {{ if .Subscriber.Attribs.contact_name }}{{ .Subscriber.Attribs.contact_name }}{{ else }}there{{ end }},'
        findings = find_unsupported_template_fields(body)
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["field"], ".Subscriber")
        self.assertEqual(findings[0]["line"], 1)
        self.assertEqual(findings[0]["hint"], "Customer")

    def test_supported_root_fields_pass(self) -> None:
        body = (
            '<p>Hello {{ if .Customer.Name }}{{ .Customer.Name }}{{ else }}there{{ end }}</p>\n'
            "<p>{{ .Customer.Email }} · {{ .Customer.Attribs.whatsapp }}</p>\n"
            "<h1>{{ .Campaign.Subject }} / {{ .Campaign.Name }}</h1>"
        )
        self.assertEqual(find_unsupported_template_fields(body), [])
        validate_campaign_body_vars(body)

    def test_nested_fields_are_not_treated_as_root(self) -> None:
        body = "{{ .Customer.Attribs.contact_name }} {{ range $i, $v := .Customer.Attribs }}x{{ end }}"
        self.assertEqual(find_unsupported_template_fields(body), [])

    def test_css_class_selectors_are_ignored(self) -> None:
        body = "<style>.btn{color:red} .Subscriber{}</style><p>{{ .Customer.Name }}</p>"
        self.assertEqual(find_unsupported_template_fields(body), [])

    def test_line_and_column_point_at_the_offending_action(self) -> None:
        body = "\n".join([
            "<html>",
            "<body>",
            '  <p>{{ if .Subscriber.Name }}x{{ end }}</p>',
        ])
        finding = find_unsupported_template_fields(body)[0]
        self.assertEqual(finding["line"], 3)

    def test_error_message_names_the_field_and_the_fix(self) -> None:
        with self.assertRaises(ValueError) as ctx:
            validate_campaign_body_vars("{{ .Subscriber.Name }}")
        message = str(ctx.exception)
        self.assertIn(".Subscriber", message)
        self.assertIn(".Customer", message)
        self.assertIn("preview", message)

    def test_empty_body_is_allowed(self) -> None:
        self.assertEqual(find_unsupported_template_fields(""), [])
        self.assertEqual(find_unsupported_template_fields(None), [])


if __name__ == "__main__":
    unittest.main()
