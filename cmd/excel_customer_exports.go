package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knadh/listmonk/models"
)

type customerExportLayout struct {
	columns   []exportColumn
	fields    []models.CustomFieldDefinition
	sensitive bool
	email     bool
}

func newCustomerExportLayout(b *exportWorkbook, fields []models.CustomFieldDefinition, sensitive, email bool) customerExportLayout {
	layout := customerExportLayout{fields: fields, sensitive: sensitive, email: email}
	layout.columns = []exportColumn{
		b.column("customers.customerCode", 24, ""), b.column("globals.fields.name", 24, ""),
	}
	if email {
		layout.columns = append(layout.columns, b.column("customers.email", 36, ""))
	}
	layout.columns = append(layout.columns, b.column("exports.customerStatus", 18, ""),
		b.column("exports.createdAtUTC", 23, "date"), b.column("exports.updatedAtUTC", 23, "date"))
	if sensitive {
		for _, field := range fields {
			label := field.Label
			if label == "" {
				label = field.Key
			}
			kind := ""
			if field.Type == "number" {
				kind = "number"
			}
			layout.columns = append(layout.columns, exportColumn{label: label, width: 28, kind: kind})
		}
		layout.columns = append(layout.columns, b.column("exports.otherAttributes", 48, ""), b.column("globals.fields.uuid", 38, ""))
	}
	return layout
}

func (b *exportWorkbook) status(value string) string {
	return b.translated("exports.status."+value, value)
}

func (layout customerExportLayout) row(b *exportWorkbook, r models.CustomerExport) ([]any, error) {
	values := []any{r.CustomerCode, r.Name}
	if layout.email {
		values = append(values, r.Email)
	}
	values = append(values, b.status(r.Status), r.CreatedAt.Time, r.UpdatedAt.Time)
	if layout.sensitive {
		var attributes map[string]any
		if r.Attribs != "" {
			decoder := json.NewDecoder(bytes.NewBufferString(r.Attribs))
			decoder.UseNumber()
			if err := decoder.Decode(&attributes); err != nil {
				return nil, fmt.Errorf("decode customer attributes: %w", err)
			}
		}
		for _, field := range layout.fields {
			value := attributes[field.Key]
			if field.Type == "number" {
				if number, ok := value.(json.Number); ok {
					// Excel retains only 15 significant digits in numeric cells.
					// Keep higher-precision values as text rather than rounding them.
					digits := strings.Map(func(ch rune) rune {
						if ch >= '0' && ch <= '9' {
							return ch
						}
						return -1
					}, number.String())
					if parsed, err := number.Float64(); err == nil && len(digits) <= 15 {
						value = parsed
					}
				}
			}
			if _, numeric := value.(float64); !numeric {
				value = exportJSONValue(value)
			}
			values = append(values, value)
			delete(attributes, field.Key)
		}
		remaining := ""
		if len(attributes) > 0 {
			remaining = exportJSONValue(attributes).(string)
		}
		values = append(values, remaining, r.UUID)
	}
	return values, nil
}

func exportJSONRows(raw json.RawMessage) ([]map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var rows []map[string]any
	switch value := value.(type) {
	case map[string]any:
		if len(value) > 0 {
			rows = append(rows, value)
		}
	case []any:
		for _, row := range value {
			record, ok := row.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid customer export row")
			}
			rows = append(rows, record)
		}
	default:
		return nil, fmt.Errorf("invalid customer export data")
	}
	return rows, nil
}

// Privacy exports contain separate tables rather than JSON blobs in cells.
// The caller applies exportable-category filtering and permission redaction
// before this formatter sees any data.
func writeCustomerDataWorkbook(b *exportWorkbook, data models.CustomerExportProfile, fields []models.CustomFieldDefinition) error {
	if len(data.Profile) > 0 {
		rows, err := exportJSONRows(data.Profile)
		if err != nil {
			return err
		}
		sensitive, email := false, false
		for _, row := range rows {
			_, hasEmail := row["email"]
			_, hasUUID := row["uuid"]
			email, sensitive = email || hasEmail, sensitive || hasUUID
		}
		layout := newCustomerExportLayout(b, fields, sensitive, email)
		sheet, err := b.addSheet("exports.profile", layout.columns)
		if err != nil {
			return err
		}
		for _, row := range rows {
			r := models.CustomerExport{
				CustomerCode: fmt.Sprint(exportJSONValue(row["customer_code"])),
				Name:         fmt.Sprint(exportJSONValue(row["name"])), Email: fmt.Sprint(exportJSONValue(row["email"])),
				UUID: fmt.Sprint(exportJSONValue(row["uuid"])), Status: fmt.Sprint(exportJSONValue(row["status"])),
			}
			attribs, _ := json.Marshal(row["attribs"])
			r.Attribs = string(attribs)
			values, err := layout.row(b, r)
			if err != nil {
				return err
			}
			dateOffset := 3
			if email {
				dateOffset++
			}
			values[dateOffset], values[dateOffset+1] = exportTimestamp(row["created_at"]), exportTimestamp(row["updated_at"])
			if err := sheet.addRow(values...); err != nil {
				return err
			}
		}
	}
	sections := []struct {
		name    string
		raw     json.RawMessage
		keys    []string
		columns []exportColumn
	}{
		{"exports.subscriptions", data.Subscriptions, []string{"name", "type", "subscription_status", "created_at"}, []exportColumn{
			b.column("exports.listName", 32, ""), b.column("globals.fields.type", 20, ""),
			b.column("exports.subscriptionStatus", 24, ""), b.column("exports.createdAtUTC", 23, "date"),
		}},
		{"exports.campaignViews", data.CampaignViews, []string{"campaign", "views"}, []exportColumn{
			b.column("campaigns.subject", 60, ""), b.column("exports.openCount", 18, "number"),
		}},
		{"exports.linkClicks", data.LinkClicks, []string{"url", "clicks"}, []exportColumn{
			b.column("exports.linkURL", 70, ""), b.column("exports.clickCount", 18, "number"),
		}},
	}
	for _, section := range sections {
		if len(section.raw) == 0 {
			continue
		}
		rows, err := exportJSONRows(section.raw)
		if err != nil {
			return err
		}
		sheet, err := b.addSheet(section.name, section.columns)
		if err != nil {
			return err
		}
		for _, row := range rows {
			values := make([]any, len(section.keys))
			for i, key := range section.keys {
				value := row[key]
				switch key {
				case "created_at":
					value = exportTimestamp(value)
				case "type", "subscription_status":
					value = b.status(fmt.Sprint(value))
				case "name":
					if value == "Private customer_list" {
						value = b.lang.T("exports.privateList")
					}
				case "views", "clicks":
					if number, ok := value.(json.Number); ok {
						if count, err := number.Int64(); err == nil {
							value = count
						}
					}
				}
				values[i] = value
			}
			if err := sheet.addRow(values...); err != nil {
				return err
			}
		}
	}
	if len(b.sheets) == 0 {
		sheet, err := b.addSheet("exports.profile", []exportColumn{b.column("exports.notes", 70, "")})
		if err != nil {
			return err
		}
		return sheet.addRow(b.lang.T("exports.noCategories"))
	}
	return nil
}
