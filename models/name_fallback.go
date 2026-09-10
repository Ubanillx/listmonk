package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NameFallback is a template-owned display rule, never a customer data update.
type NameFallback struct {
	Enabled       bool     `json:"enabled"`
	Value         string   `json:"value"`
	InvalidValues []string `json:"invalid_values"`
}

func (f NameFallback) Validate() error {
	if f.Enabled && strings.TrimSpace(f.Value) == "" {
		return fmt.Errorf("name_fallback.value is required when enabled")
	}
	if len([]rune(f.Value)) > 200 || strings.ContainsAny(f.Value, "\r\n") {
		return fmt.Errorf("name_fallback.value must be a single line of at most 200 characters")
	}
	if len(f.InvalidValues) > 50 {
		return fmt.Errorf("name_fallback supports at most 50 invalid values")
	}
	for _, value := range f.InvalidValues {
		if len([]rune(value)) > 200 || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("name_fallback invalid values must be single lines of at most 200 characters")
		}
	}
	return nil
}

func (f NameFallback) Resolve(name string) string {
	if !f.Enabled {
		return name
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return strings.TrimSpace(f.Value)
	}
	for _, invalid := range f.InvalidValues {
		if strings.EqualFold(name, strings.TrimSpace(invalid)) {
			return strings.TrimSpace(f.Value)
		}
	}
	return name
}

func (f NameFallback) ValueForDB() string {
	b, _ := json.Marshal(f)
	return string(b)
}

// Value is already the public text field, so SQL writes explicitly use ValueForDB.
var _ interface{ Scan(any) error } = (*NameFallback)(nil)

func (f *NameFallback) Scan(src any) error {
	*f = NameFallback{}
	if src == nil {
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("invalid name_fallback database type %T", src)
	}
	return json.Unmarshal(b, f)
}
