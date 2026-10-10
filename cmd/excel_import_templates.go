package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Import templates contain no resource data. Authentication is sufficient;
// submitting an import continues to require the existing action permissions.
func (a *App) DownloadImportTemplate(c echo.Context) error {
	kind := c.Param("kind")
	if kind != "users" && kind != "members" {
		return echo.NewHTTPError(http.StatusNotFound, "unknown import template")
	}
	book, err := a.newExportWorkbook(c)
	if err != nil {
		return err
	}
	defer book.file.Close()
	keys := []string{"username", "name", "password", "email", "user_role", "customer_list_role", "status"}
	labels := []string{"users.username", "globals.fields.name", "users.password", "customers.email", "users.userRole", "users.customerListRole", "globals.fields.status"}
	widths := []float64{24, 24, 28, 36, 28, 28, 18}
	required := map[string]bool{"username": true, "password": true, "email": true, "user_role": true}
	title := "exports.users"
	if kind == "members" {
		keys = []string{"account", "role"}
		labels = []string{"organizations.account", "organizations.role"}
		widths = []float64{36, 24}
		required = map[string]bool{"account": true}
		title = "exports.members"
	}
	columns := make([]exportColumn, len(keys))
	for i, key := range labels {
		columns[i] = book.column(key, widths[i], "")
	}
	if _, err := book.addSheet(title, columns); err != nil {
		return err
	}
	notes, err := book.addSheet("exports.instructions", []exportColumn{
		book.column("exports.field", 28, ""), book.column("exports.required", 18, ""), book.column("exports.notes", 80, ""),
	})
	if err != nil {
		return err
	}
	for i, key := range keys {
		value := book.lang.T("exports.optional")
		if required[key] {
			value = book.lang.T("exports.required")
		}
		if err := notes.addRow(columns[i].label, value, book.lang.T("exports.template."+key)); err != nil {
			return err
		}
	}
	return book.download(c, kind+"-import-template.xlsx")
}
