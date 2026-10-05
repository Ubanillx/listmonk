package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

type poolExportContact struct {
	poolID    int64
	contactID int64
}

// Selection only narrows the authorized, filtered export rows. Aggregate
// selections use both IDs because a contact can belong to more than one pool.
func parsePoolExportSelection(params url.Values, aggregate bool) (map[poolExportContact]bool, error) {
	values, present := params["contact"]
	if !present {
		return nil, nil
	}
	if len(values) == 0 || len(values) > 1000 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid pool contact selection")
	}
	selected := make(map[poolExportContact]bool, len(values))
	for _, value := range values {
		parts := strings.Split(value, ":")
		if len(parts) != 2 {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid pool contact selection")
		}
		poolID, poolErr := strconv.ParseInt(parts[0], 10, 64)
		contactID, contactErr := strconv.ParseInt(parts[1], 10, 64)
		if poolErr != nil || contactErr != nil || contactID <= 0 || (aggregate && poolID <= 0) || (!aggregate && poolID != 0) {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid pool contact selection")
		}
		selected[poolExportContact{poolID, contactID}] = true
	}
	return selected, nil
}

func poolExportIncludes(selected map[poolExportContact]bool, poolID, contactID int64) bool {
	return selected == nil || selected[poolExportContact{poolID, contactID}]
}
