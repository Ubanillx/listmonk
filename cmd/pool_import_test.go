package main

import (
	"io"
	"testing"
)

func TestParsePoolAllocationRows(t *testing.T) {
	values := [][]string{
		{"C-001", "One@Example.com"},
		{"", ""},
		{"C-002", ""},
	}
	index := 0
	rows, err := parsePoolAllocationRows([]string{"\ufeffcustomer_code", "email"}, func() ([]string, error) {
		if index >= len(values) {
			return nil, io.EOF
		}
		row := values[index]
		index++
		return row, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Row != 2 || rows[0].CustomerCode != "C-001" || rows[0].Email != "One@Example.com" {
		t.Fatalf("unexpected parsed rows: %#v", rows)
	}
	if rows[1].Row != 4 || rows[1].CustomerCode != "C-002" {
		t.Fatalf("unexpected row numbering: %#v", rows[1])
	}
}

func TestParsePoolAllocationRowsRequiresColumns(t *testing.T) {
	_, err := parsePoolAllocationRows([]string{"code", "address"}, func() ([]string, error) { return nil, io.EOF })
	if err == nil {
		t.Fatal("expected missing-column error")
	}
}
