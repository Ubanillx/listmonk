package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/knadh/goyesql/v2"
)

func TestCreateCustomerListSQLArgumentOrder(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating customer list SQL")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "queries", "customer_lists.sql"))
	if err != nil {
		t.Fatalf("read customer list SQL: %v", err)
	}
	queries, err := goyesql.ParseBytes(body)
	if err != nil {
		t.Fatalf("parse customer list SQL: %v", err)
	}
	query, ok := queries["create-customer-list"]
	if !ok {
		t.Fatal("create customer list query is not registered")
	}
	if !strings.Contains(query.Query, "VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)") {
		t.Error("create customer list query must keep mask_emails and workspace scope arguments in call order")
	}
}
