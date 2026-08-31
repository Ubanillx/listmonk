package core

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/listmonk/models"
)

// ValidateCustomFieldValues validates configured account attributes while
// preserving unknown legacy attributes.
func (c *Core) ValidateCustomFieldValues(values models.JSON) error {
	s, err := c.GetSettings()
	if err != nil {
		return err
	}
	for _, f := range s.CustomFields {
		if !f.Active {
			continue
		}
		v, ok := values[f.Key]
		if !ok || customFieldValueMissing(v) {
			if f.Required {
				return fmt.Errorf("field %s is required", f.Label)
			}
			continue
		}
		switch f.Type {
		case "number":
			if _, err := strconv.ParseFloat(fmt.Sprint(v), 64); err != nil {
				return fmt.Errorf("field %s must be a number", f.Label)
			}
		case "url":
			u, err := url.Parse(fmt.Sprint(v))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("field %s must be a valid URL", f.Label)
			}
		case "date":
			if _, err := time.Parse("2006-01-02", fmt.Sprint(v)); err != nil {
				return fmt.Errorf("field %s must be a valid date", f.Label)
			}
		case "select":
			valid := false
			for _, option := range f.Options {
				if option == fmt.Sprint(v) {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("field %s has an invalid option", f.Label)
			}
		case "multi_select":
			items, err := customFieldOptions(v)
			if err != nil {
				return fmt.Errorf("field %s must contain one or more options", f.Label)
			}
			for _, item := range items {
				valid := false
				for _, option := range f.Options {
					if option == item {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("field %s has an invalid option", f.Label)
				}
			}
			values[f.Key] = items
		case "checkbox":
			checked, err := strconv.ParseBool(strings.TrimSpace(fmt.Sprint(v)))
			if err != nil {
				return fmt.Errorf("field %s must be true or false", f.Label)
			}
			values[f.Key] = checked
		}
	}
	return nil
}

func customFieldValueMissing(v any) bool {
	if v == nil {
		return true
	}
	if items, ok := v.([]string); ok {
		return len(items) == 0
	}
	if items, ok := v.([]any); ok {
		return len(items) == 0
	}
	return strings.TrimSpace(fmt.Sprint(v)) == ""
}

func customFieldOptions(v any) ([]string, error) {
	var raw []any
	switch value := v.(type) {
	case []any:
		raw = value
	case []string:
		raw = make([]any, len(value))
		for i := range value {
			raw[i] = value[i]
		}
	case string:
		for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' }) {
			raw = append(raw, item)
		}
	default:
		return nil, fmt.Errorf("invalid options")
	}
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
			items = append(items, s)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("empty options")
	}
	return items, nil
}
