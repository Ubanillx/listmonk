package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/models"
	"github.com/knadh/stuffbin"
	"github.com/labstack/echo/v4"
	"github.com/xuri/excelize/v2"
)

func exportTestLanguage(t *testing.T, code string) *i18n.I18n {
	t.Helper()
	raw, err := os.ReadFile("../i18n/" + code + ".json")
	if err != nil {
		t.Fatal(err)
	}
	lang, err := i18n.New(raw)
	if err != nil {
		t.Fatal(err)
	}
	return lang
}

func openExportTestWorkbook(t *testing.T, book *exportWorkbook) *excelize.File {
	t.Helper()
	if err := book.finish(); err != nil {
		t.Fatal(err)
	}
	raw, err := book.file.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	file, err := excelize.OpenReader(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func TestExcelExportPreservesIdentifiersLiteralTextDatesAndSheetOverflow(t *testing.T) {
	book, err := newExportWorkbook(context.Background(), exportTestLanguage(t, "zh-CN"))
	if err != nil {
		t.Fatal(err)
	}
	defer book.file.Close()
	sheet, err := book.addSheet("exports.customers", []exportColumn{
		book.column("customers.customerCode", 24, ""), book.column("globals.fields.name", 30, ""), book.column("exports.createdAtUTC", 23, "date"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the real rollover path without writing a million fixture rows.
	sheet.maxRows = 3
	stamp := time.Date(2026, 10, 9, 20, 30, 0, 0, time.FixedZone("CST", 8*3600))
	for _, code := range []string{"00001234567890123456", "00002", "00003"} {
		if err := sheet.addRow(code, "=HYPERLINK(\"https://example.invalid\")", stamp); err != nil {
			t.Fatal(err)
		}
	}
	file := openExportTestWorkbook(t, book)
	if got := file.GetSheetList(); len(got) != 2 || got[0] != "私域客户" || got[1] != "私域客户 (2)" {
		t.Fatalf("sheet rollover = %v", got)
	}
	for _, name := range file.GetSheetList() {
		panes, err := file.GetPanes(name)
		if err != nil || !panes.Freeze || panes.YSplit != 1 {
			t.Fatalf("frozen header = %+v, %v", panes, err)
		}
		tables, err := file.GetTables(name)
		if err != nil || len(tables) != 1 {
			t.Fatalf("filterable table = %+v, %v", tables, err)
		}
	}
	rows, err := file.GetRows("私域客户")
	if err != nil || len(rows) != 3 || rows[0][0] != book.lang.T("customers.customerCode") || rows[1][0] != "00001234567890123456" || rows[1][2] != "2026-10-09 12:30:00" {
		t.Fatalf("workbook values = %#v, %v", rows, err)
	}
	if formula, err := file.GetCellFormula("私域客户", "B2"); err != nil || formula != "" {
		t.Fatalf("user text became a formula: %q, %v", formula, err)
	}
	if value, _ := file.GetCellValue("私域客户", "B2"); !strings.HasPrefix(value, "=HYPERLINK") {
		t.Fatalf("literal text changed: %q", value)
	}
}

func TestExcelPrivacyCategoriesAndRedactionRemainSeparate(t *testing.T) {
	for _, code := range []string{"en", "zh-CN", "zh-TW"} {
		t.Run(code, func(t *testing.T) {
			book, err := newExportWorkbook(context.Background(), exportTestLanguage(t, code))
			if err != nil {
				t.Fatal(err)
			}
			defer book.file.Close()
			profile := json.RawMessage(`[{"customer_code":"000123","name":"Alice","email":"secret@example.invalid","uuid":"sensitive-uuid","attribs":{"secret":"hidden"},"status":"enabled","created_at":"2026-10-09T20:30:00+08:00"}]`)
			redacted, err := redactCustomerExportProfile(profile)
			if err != nil {
				t.Fatal(err)
			}
			data := models.CustomerExportProfile{
				Profile: redacted, Subscriptions: json.RawMessage(`[{"name":"Private customer_list","type":"private","subscription_status":"confirmed"}]`),
				CampaignViews: json.RawMessage(`[{"campaign":"Subject","views":3}]`), LinkClicks: json.RawMessage(`[{"url":"https://example.invalid","clicks":2}]`),
			}
			filterCustomerExportables(&data, map[string]bool{"profile": true, "subscriptions": true, "campaign_views": true})
			if err := writeCustomerDataWorkbook(book, data, nil); err != nil {
				t.Fatal(err)
			}
			file := openExportTestWorkbook(t, book)
			if len(file.GetSheetList()) != 3 {
				t.Fatalf("disabled category exported: %v", file.GetSheetList())
			}
			for _, name := range file.GetSheetList() {
				rows, err := file.GetRows(name)
				if err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(rows)
				for _, secret := range []string{"secret@example.invalid", "sensitive-uuid", "hidden", "https://example.invalid"} {
					if strings.Contains(string(encoded), secret) {
						t.Fatalf("redacted/disabled data leaked: %s", encoded)
					}
				}
			}
			rows, _ := file.GetRows(book.lang.T("exports.profile"))
			if rows[1][0] != "000123" || rows[1][3] != "2026-10-09 12:30:00" {
				t.Fatalf("profile values = %v", rows)
			}
		})
	}
}

func TestExcelExportRejectsOversizedCellsWithoutCommittingDownload(t *testing.T) {
	book, err := newExportWorkbook(context.Background(), exportTestLanguage(t, "en"))
	if err != nil {
		t.Fatal(err)
	}
	defer book.file.Close()
	sheet, err := book.addSheet("exports.profile", []exportColumn{book.column("globals.fields.name", 24, "")})
	if err != nil {
		t.Fatal(err)
	}
	if err := sheet.addRow(strings.Repeat("A", 32768)); err == nil {
		t.Fatal("oversized text silently truncated")
	}
}

func TestExcelTemplateLanguageAndDownloadContract(t *testing.T) {
	fs, err := stuffbin.NewLocalFS("/", "../i18n:/i18n")
	if err != nil {
		t.Fatal(err)
	}
	app := &App{fs: fs, i18n: exportTestLanguage(t, "en")}
	for _, kind := range []string{"users", "members"} {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/import-templates/"+kind+"?lang=zh-CN", nil), rec)
		c.SetParamNames("kind")
		c.SetParamValues(kind)
		if err := app.DownloadImportTemplate(c); err != nil {
			t.Fatal(err)
		}
		if rec.Header().Get(echo.HeaderContentType) != excelContentType || !strings.Contains(rec.Header().Get(echo.HeaderContentDisposition), ".xlsx") || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("download headers = %v", rec.Header())
		}
		file, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := file.GetRows(file.GetSheetName(0))
		if kind == "users" && rows[0][0] != "用户名" || kind == "members" && rows[0][0] != "账号" {
			t.Fatalf("localized template headers = %v", rows)
		}
		if len(file.GetSheetList()) != 2 || file.GetSheetName(1) != "填写说明" {
			t.Fatalf("template instructions missing: %v", file.GetSheetList())
		}
		file.Close()
	}
	for _, lang := range []string{"../en", "en/../../", "not-a-language"} {
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/?lang="+lang, nil), httptest.NewRecorder())
		if _, err := app.exportLanguage(c); err == nil {
			t.Fatalf("invalid language accepted: %q", lang)
		}
	}
	if app.i18n.Code != "en" {
		t.Fatal("request changed global language")
	}
}
