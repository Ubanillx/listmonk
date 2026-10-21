package main

import (
	"fmt"
	"strings"

	"github.com/knadh/listmonk/internal/bestedm"
)

type providerFailureCause struct {
	Type   string `json:"error_type"`
	Detail string `json:"error_type_dec"`
	Count  int    `json:"error_count"`
}

type providerFailureSummary struct {
	Email   string                  `json:"email"`
	Count   int                     `json:"error_count"`
	Errors  []providerFailureCause  `json:"errors"`
	Matches []providerFailureTarget `json:"matches"`
}

// Count returned occurrences, not inferred SMTP attempts. Keep all distinct
// classifications and descriptions, in first-seen order, for each address.
func summarizeProviderFailures(rows []bestedm.Row) []providerFailureSummary {
	out := make([]providerFailureSummary, 0)
	addresses := make(map[string]int)
	causes := make([]map[[2]string]int, 0)
	for i, row := range rows {
		email := strings.TrimSpace(row["email"])
		key := strings.ToLower(email)
		if key == "" {
			key = fmt.Sprintf("\x00%d", i)
		}
		index, exists := addresses[key]
		if !exists {
			index = len(out)
			addresses[key] = index
			out = append(out, providerFailureSummary{Email: email, Errors: make([]providerFailureCause, 0), Matches: make([]providerFailureTarget, 0)})
			causes = append(causes, make(map[[2]string]int))
		}
		summary := &out[index]
		summary.Count++
		cause := [2]string{row["error_type"], row["error_type_dec"]}
		if at, exists := causes[index][cause]; exists {
			summary.Errors[at].Count++
		} else {
			causes[index][cause] = len(summary.Errors)
			summary.Errors = append(summary.Errors, providerFailureCause{Type: cause[0], Detail: cause[1], Count: 1})
		}
	}
	return out
}
