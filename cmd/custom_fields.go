package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

var customFieldKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

const customFieldsSettingKey = "subscriber.custom_fields"

var customFieldTypes = map[string]bool{
	"text": true, "textarea": true, "number": true, "url": true, "date": true, "select": true,
	"multi_select": true, "checkbox": true,
}

type customFieldResponse struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description,omitempty"`
	Active      bool     `json:"active"`
	System      bool     `json:"system"`
	Placeholder string   `json:"placeholder"`
	Locked      bool     `json:"locked"`
}

// GetCustomFields returns system subscriber fields and the administrator's
// globally configured fields. It is intentionally available to every logged-in
// user so subscriber forms, imports and campaign editors can show mappings.
func (a *App) GetCustomFields(c echo.Context) error {
	s, err := a.core.GetSettings()
	if err != nil {
		return err
	}
	locked, err := a.customFieldsLocked()
	if err != nil {
		return err
	}
	out := []customFieldResponse{
		{Key: "email", Label: a.i18n.T("subscribers.email"), Type: "email", Active: true, System: true, Placeholder: "{{ .Subscriber.Email }}", Locked: locked},
		{Key: "name", Label: a.i18n.T("globals.fields.name"), Type: "text", Active: true, System: true, Placeholder: "{{ .Subscriber.Name }}", Locked: locked},
	}
	for _, f := range s.CustomFields {
		out = append(out, customFieldResponse{Key: f.Key, Label: f.Label, Type: f.Type, Required: f.Required,
			Options: f.Options, Description: f.Description, Active: f.Active, Placeholder: "{{ .Subscriber.Attribs." + f.Key + " }}", Locked: locked})
	}
	return c.JSON(http.StatusOK, okResp{out})
}

type customFieldRequest struct {
	models.CustomFieldDefinition
	OldKey string `json:"old_key"`
}

func (a *App) saveCustomFields(c echo.Context, mutate func([]models.CustomFieldDefinition) ([]models.CustomFieldDefinition, error)) error {
	if !auth.GetUser(c).IsPlatformAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "platform administrator required")
	}
	locked, err := a.customFieldsLocked()
	if err != nil {
		return err
	}
	if locked {
		return echo.NewHTTPError(http.StatusConflict, "custom fields cannot be changed while a campaign is running")
	}
	s, err := a.core.GetSettings()
	if err != nil {
		return err
	}
	out, err := mutate(s.CustomFields)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := a.core.UpdateSettingsByKey(customFieldsSettingKey, b); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, okResp{out})
}

// customFieldsLocked uses the persisted campaign state rather than only the
// in-memory worker list. This keeps the rule intact while a process is being
// restarted or when a second worker owns the active campaign.
func (a *App) customFieldsLocked() (bool, error) {
	var locked bool
	if err := a.db.Get(&locked, `SELECT EXISTS(SELECT 1 FROM campaigns WHERE status = 'running')`); err != nil {
		return false, echo.NewHTTPError(http.StatusInternalServerError, "checking running campaigns")
	}
	return locked, nil
}

func validateCustomField(f *models.CustomFieldDefinition) error {
	f.Key = strings.ToLower(strings.TrimSpace(f.Key))
	f.Label = strings.TrimSpace(f.Label)
	f.Type = strings.ToLower(strings.TrimSpace(f.Type))
	if !customFieldKey.MatchString(f.Key) || f.Key == "email" || f.Key == "name" || f.Key == "attributes" || f.Key == "attribs" {
		return fmt.Errorf("invalid or reserved field key")
	}
	if f.Label == "" || len(f.Label) > stdInputMaxLen {
		return fmt.Errorf("field label is required")
	}
	if !customFieldTypes[f.Type] {
		return fmt.Errorf("unsupported field type: %s", f.Type)
	}
	if (f.Type == "select" || f.Type == "multi_select") && len(f.Options) == 0 {
		return fmt.Errorf("select fields require options")
	}
	for i := range f.Options {
		f.Options[i] = strings.TrimSpace(f.Options[i])
		if f.Options[i] == "" {
			return fmt.Errorf("field options cannot be empty")
		}
	}
	return nil
}

func (a *App) CreateCustomField(c echo.Context) error {
	var req models.CustomFieldDefinition
	if err := c.Bind(&req); err != nil {
		return err
	}
	return a.saveCustomFields(c, func(fields []models.CustomFieldDefinition) ([]models.CustomFieldDefinition, error) {
		if err := validateCustomField(&req); err != nil {
			return nil, err
		}
		req.Active = true
		for _, f := range fields {
			if f.Key == req.Key {
				return nil, fmt.Errorf("field key already exists")
			}
		}
		return append(fields, req), nil
	})
}

func (a *App) UpdateCustomField(c echo.Context) error {
	id := c.Param("key")
	var req models.CustomFieldDefinition
	if err := c.Bind(&req); err != nil {
		return err
	}
	return a.saveCustomFields(c, func(fields []models.CustomFieldDefinition) ([]models.CustomFieldDefinition, error) {
		idx := -1
		for i := range fields {
			if fields[i].Key == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("field not found")
		}
		if req.Key != "" && strings.ToLower(strings.TrimSpace(req.Key)) != id {
			return nil, fmt.Errorf("field key cannot be changed")
		}
		req.Key = id
		if err := validateCustomField(&req); err != nil {
			return nil, err
		}
		for i, f := range fields {
			if i != idx && f.Key == req.Key {
				return nil, fmt.Errorf("field key already exists")
			}
		}
		fields[idx] = req
		return fields, nil
	})
}

func (a *App) DeleteCustomField(c echo.Context) error {
	id := c.Param("key")
	return a.saveCustomFields(c, func(fields []models.CustomFieldDefinition) ([]models.CustomFieldDefinition, error) {
		found := false
		for i := range fields {
			if fields[i].Key == id {
				fields[i].Active = false
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("field not found")
		}
		return fields, nil
	})
}

// validateCustomFieldValues validates only configured fields; arbitrary legacy
// attributes remain supported for backwards compatibility.
func (a *App) validateCustomFieldValues(values models.JSON) error {
	return a.core.ValidateCustomFieldValues(values)
}
