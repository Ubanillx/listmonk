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

func TestParsePoolContactImportRowsTemplate(t *testing.T) {
	values := [][]string{
		{"AE-20004", "JAGATHISH PICHAIYAPPA", "JAGAH.P@SANIPEXGROUP.COM", "分表1", "ignored"},
		{"AE-20005", "Contact Two", "two@example.com", "分表2", "ignored"},
	}
	index := 0
	rows, err := parsePoolContactImportRows([]string{"客户编号", "姓名", "邮箱", "分配部门", "品牌名称"}, func() ([]string, error) {
		if index >= len(values) {
			return nil, io.EOF
		}
		row := values[index]
		index++
		return row, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Row != 2 || rows[0].CustomerCode != "AE-20004" || rows[0].Name != "JAGATHISH PICHAIYAPPA" || rows[0].Email != "JAGAH.P@SANIPEXGROUP.COM" || rows[0].AllocationDepartment != "分表1" {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
}

func TestParsePoolContactImportRowsFieldMap(t *testing.T) {
	values := [][]string{{"extra", "D-001", "d@example.com", "部门甲", "张三"}}
	index := 0
	rows, err := parsePoolContactImportRows([]string{"extra", "code", "mail", "dept", "person"}, func() ([]string, error) {
		if index >= len(values) {
			return nil, io.EOF
		}
		row := values[index]
		index++
		return row, nil
	}, map[string]string{
		"customer_code":         "code",
		"name":                  "person",
		"email":                 "mail",
		"allocation_department": "dept",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].CustomerCode != "D-001" || rows[0].Name != "张三" || rows[0].Email != "d@example.com" || rows[0].AllocationDepartment != "部门甲" {
		t.Fatalf("unexpected mapped row: %#v", rows)
	}
}

func TestParsePoolContactImportRowsRequiresTemplateFields(t *testing.T) {
	_, err := parsePoolContactImportRows([]string{"客户编号", "姓名", "邮箱"}, func() ([]string, error) { return nil, io.EOF }, nil)
	if err == nil {
		t.Fatal("expected missing allocation department error")
	}
}
