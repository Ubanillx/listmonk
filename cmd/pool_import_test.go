package main

import (
	"bytes"
	"encoding/csv"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
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

func TestPoolImportPrefersExplicitAllocationDepartmentColumn(t *testing.T) {
	read := false
	rows, err := parsePoolContactImportRows(
		[]string{"客户编号", "姓名", "邮箱", "部门", "分配部门"},
		func() ([]string, error) {
			if read {
				return nil, io.EOF
			}
			read = true
			return []string{"C-001", "Contact", "one@example.com", "Source department", "Routing department"}, nil
		}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AllocationDepartment != "Routing department" {
		t.Fatalf("unexpected allocation department: %#v", rows)
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

func TestPoolImportReplyToOptionalAndMapped(t *testing.T) {
	for _, tc := range []struct {
		header   []string
		values   []string
		fieldMap map[string]string
		want     string
	}{
		{[]string{"客户编号", "姓名", "邮箱", "分配部门"}, []string{"A", "One", "one@example.com", "Sales"}, nil, ""},
		{[]string{"客户编号", "姓名", "邮箱", "分配部门", "回信邮箱"}, []string{"A", "One", "one@example.com", "Sales", " replies@example.com "}, nil, "replies@example.com"},
		{[]string{"客户编号", "姓名", "邮箱", "分配部门", "route"}, []string{"A", "One", "one@example.com", "Sales", "replies@example.com"}, map[string]string{"reply_to": "E"}, "replies@example.com"},
	} {
		read := false
		rows, err := parsePoolContactImportRows(tc.header, func() ([]string, error) {
			if read {
				return nil, io.EOF
			}
			read = true
			return tc.values, nil
		}, tc.fieldMap)
		if err != nil || len(rows) != 1 || rows[0].ReplyTo != tc.want {
			t.Fatalf("reply_to import: %+v, %v; want %q", rows, err, tc.want)
		}
	}
}

func TestSplitPoolImportEmails(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "separators and punctuation",
			input: "first@example.com; second@example.com.\nthird@example.com",
			want:  []string{"first@example.com", "second@example.com", "third@example.com"},
		},
		{
			name:  "embedded note",
			input: "first@example.com <联系人>，请抄送 second@example.com",
			want:  []string{"first@example.com", "second@example.com"},
		},
		{
			name:  "full width punctuation",
			input: "FIRST＠EXAMPLE．COM；second@example.com",
			want:  []string{"FIRST@EXAMPLE.COM", "second@example.com"},
		},
		{
			name:  "unmatched value is retained for validation",
			input: "not an email",
			want:  []string{"not an email"},
		},
		{
			name:  "blank value remains one row",
			input: "  ",
			want:  []string{""},
		},
		{
			name:  "malformed local comma is retained",
			input: "first,last@example.com",
			want:  []string{"first,last@example.com"},
		},
		{
			name:  "earlier address does not split a malformed local part",
			input: "one@example.com lucas,alves@example.com",
			want:  []string{"one@example.com", "lucas,alves@example.com"},
		},
		{
			name:  "commas after spaces or annotations",
			input: "one@example.com ,two@example.com <联系人>,three@example.com",
			want:  []string{"one@example.com", "two@example.com", "three@example.com"},
		},
		{
			name:  "broken domain is retained",
			input: "user@public 1 example.com",
			want:  []string{"user@public 1 example.com"},
		},
		{
			name:  "whitespace separated addresses",
			input: "bad@nodot valid@example.com",
			want:  []string{"bad@nodot", "valid@example.com"},
		},
		{
			name:  "line and pipe separators",
			input: "one@example.com\r\ntwo@example.com|three@example.com",
			want:  []string{"one@example.com", "two@example.com", "three@example.com"},
		},
		{
			name:  "slash separator preserves local part slashes",
			input: "sales/emea@example.com/other@example.com",
			want:  []string{"sales/emea@example.com", "other@example.com"},
		},
		{
			name:  "quoted address is kept intact",
			input: `"Last, First" <first@example.com>`,
			want:  []string{"first@example.com"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitPoolImportEmails(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %#v, want %#v", got, tc.want)
				}
			}
		})
	}
}

func TestParsePoolContactImportFileExpandsEmails(t *testing.T) {
	values := [][]string{
		{"客户编号", "姓名", "邮箱", "部门", "分配部门", "回信邮箱"},
		{"C-001", "Contact", "one@example.com;two@example.com\r\n|three@example.com", "Source department", "Sales", "reply@example.com"},
		{},
		{"C-002", "Second", "lucas,alves@example.com;valid@example.com", "Source department", "Sales", "second-reply@example.com"},
	}
	var csvContent bytes.Buffer
	writer := csv.NewWriter(&csvContent)
	if err := writer.WriteAll(values); err != nil {
		t.Fatal(err)
	}
	workbook := excelize.NewFile()
	defer workbook.Close()
	for i, row := range values {
		if err := workbook.SetSheetRow("Sheet1", "A"+strconv.Itoa(i+1), &row); err != nil {
			t.Fatal(err)
		}
	}
	xlsxContent, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		filename string
		content  []byte
	}{
		{"contacts.csv", csvContent.Bytes()},
		{"contacts.xlsx", xlsxContent.Bytes()},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			rows, err := parsePoolContactImportFile(multipartFileHeader(t, tc.filename, tc.content), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 5 {
				t.Fatalf("got %d expanded rows, want 5", len(rows))
			}
			for i, row := range rows {
				if row.AllocationDepartment != "Sales" {
					t.Fatalf("wrong routing department: %#v", row)
				}
				if i < 3 && (row.Row != 2 || row.CustomerCode != "C-001" || row.Name != "Contact" || row.ReplyTo != "reply@example.com") {
					t.Fatalf("first source row's fields were not reused: %#v", row)
				}
				wantSourceRow := 3
				if strings.HasSuffix(tc.filename, ".xlsx") {
					wantSourceRow = 4
				}
				if i >= 3 && (row.Row != wantSourceRow || row.CustomerCode != "C-002" || row.Name != "Second" || row.ReplyTo != "second-reply@example.com") {
					t.Fatalf("second source row's fields were not reused: %#v", row)
				}
			}
			want := []string{"one@example.com", "two@example.com", "three@example.com", "lucas,alves@example.com", "valid@example.com"}
			for i, email := range want {
				if rows[i].Email != email {
					t.Fatalf("row %d email = %q, want %q", i, rows[i].Email, email)
				}
			}
		})
	}
}

func TestParsePoolContactImportRowsCapsExpandedRows(t *testing.T) {
	emails := make([]string, 100001)
	for i := range emails {
		emails[i] = "user" + strconv.Itoa(i) + "@example.com"
	}
	value := strings.Join(emails, ";")
	read := false
	_, err := parsePoolContactImportRows(
		[]string{"客户编号", "姓名", "邮箱", "分配部门"},
		func() ([]string, error) {
			if read {
				return nil, io.EOF
			}
			read = true
			return []string{"C-001", "Contact", value, "Sales"}, nil
		}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "100000") {
		t.Fatalf("expected expanded-row limit error, got %v", err)
	}
}

func TestParsePoolContactImportRowsExpandsEmailsAndReusesFields(t *testing.T) {
	values := [][]string{{"C-001", "Contact", "one@example.com;two@example.com. note three@example.com", "Sales", "reply@example.com"}}
	index := 0
	rows, err := parsePoolContactImportRows(
		[]string{"客户编号", "姓名", "邮箱", "分配部门", "回信邮箱"},
		func() ([]string, error) {
			if index >= len(values) {
				return nil, io.EOF
			}
			row := values[index]
			index++
			return row, nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %#v", len(rows), rows)
	}
	for _, row := range rows {
		if row.Row != 2 || row.CustomerCode != "C-001" || row.Name != "Contact" ||
			row.AllocationDepartment != "Sales" || row.ReplyTo != "reply@example.com" {
			t.Fatalf("source fields were not reused: %#v", row)
		}
	}
	if rows[0].Email != "one@example.com" || rows[1].Email != "two@example.com" || rows[2].Email != "three@example.com" {
		t.Fatalf("unexpected emails: %#v", rows)
	}
}
