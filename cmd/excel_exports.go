package main

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/knadh/listmonk/internal/i18n"
	"github.com/labstack/echo/v4"
	"github.com/xuri/excelize/v2"
)

const excelContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

type exportColumn struct {
	label string
	width float64
	kind  string // text (default), number, or date
}

// Export workbooks are request-local: selecting a language never mutates the
// application's translator or another user's concurrent download.
type exportWorkbook struct {
	file   *excelize.File
	lang   *i18n.I18n
	ctx    context.Context
	sheets []*exportSheet
	styles [2]map[string]int
}

type exportSheet struct {
	book    *exportWorkbook
	stream  *excelize.StreamWriter
	columns []exportColumn
	base    string
	name    string
	row     int
	part    int
	index   int
	maxRows int
}

func (a *App) exportLanguage(c echo.Context) (*i18n.I18n, error) {
	code := strings.TrimSpace(c.QueryParam("lang"))
	if code == "" {
		code = strings.TrimSpace(c.Request().Header.Get("X-Listmonk-Language"))
	}
	if code == "" {
		return a.i18n, nil
	}
	if len(code) > 32 || strings.ContainsAny(code, `/\\.`) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid export language")
	}
	for _, ch := range code {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "invalid export language")
		}
	}
	lang, _, err := getI18nLang(code, a.fs)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "unknown export language")
	}
	return lang, nil
}

func (a *App) newExportWorkbook(c echo.Context) (*exportWorkbook, error) {
	lang, err := a.exportLanguage(c)
	if err != nil {
		return nil, err
	}
	return newExportWorkbook(c.Request().Context(), lang)
}

func newExportWorkbook(ctx context.Context, lang *i18n.I18n) (*exportWorkbook, error) {
	b := &exportWorkbook{file: excelize.NewFile(), lang: lang, ctx: ctx}
	for band := range b.styles {
		b.styles[band] = make(map[string]int)
		for _, kind := range []string{"text", "number", "date", "header"} {
			style := &excelize.Style{
				Font:      &excelize.Font{Family: "Calibri", Size: 11, Color: "243746"},
				Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
			}
			if band == 1 {
				style.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F0F5FA"}}
			}
			switch kind {
			case "text":
				style.NumFmt = 49
			case "date":
				format := "yyyy-mm-dd hh:mm:ss"
				style.CustomNumFmt = &format
			case "header":
				style.Font.Bold, style.Font.Color = true, "FFFFFF"
				style.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"24476A"}}
			}
			id, err := b.file.NewStyle(style)
			if err != nil {
				b.file.Close()
				return nil, err
			}
			b.styles[band][kind] = id
		}
	}
	return b, nil
}

func (b *exportWorkbook) translated(key, fallback string) string {
	value := b.lang.T(key)
	if value == key {
		return fallback
	}
	return value
}

func (b *exportWorkbook) column(key string, width float64, kind string) exportColumn {
	return exportColumn{label: b.lang.T(key), width: width, kind: kind}
}

func (b *exportWorkbook) addSheet(key string, columns []exportColumn) (*exportSheet, error) {
	if len(columns) == 0 || len(columns) > excelize.MaxColumns {
		return nil, fmt.Errorf("invalid export column count: %d", len(columns))
	}
	s := &exportSheet{book: b, columns: columns, base: b.lang.T(key), maxRows: excelize.TotalRows}
	if err := s.start(); err != nil {
		return nil, err
	}
	b.sheets = append(b.sheets, s)
	return s, nil
}

func (s *exportSheet) start() error {
	s.part++
	base := strings.Map(func(ch rune) rune {
		if ch < 32 || strings.ContainsRune(`:/\\?*[]`, ch) {
			return ' '
		}
		return ch
	}, strings.Trim(s.base, " '"))
	if base == "" {
		base = "Export"
	}
	for suffix := s.part; ; suffix++ {
		end := ""
		if suffix > 1 {
			end = fmt.Sprintf(" (%d)", suffix)
		}
		runes := []rune(base)
		if len(runes) > 31-len(end) {
			runes = runes[:31-len(end)]
		}
		s.name = string(runes) + end
		if index, _ := s.book.file.GetSheetIndex(s.name); index == -1 {
			break
		}
	}
	if len(s.book.sheets) == 0 && s.part == 1 {
		if err := s.book.file.SetSheetName("Sheet1", s.name); err != nil {
			return err
		}
	} else if _, err := s.book.file.NewSheet(s.name); err != nil {
		return err
	}
	s.index = len(s.book.file.GetSheetList())
	stream, err := s.book.file.NewStreamWriter(s.name)
	if err != nil {
		return err
	}
	s.stream, s.row = stream, 1
	if err := stream.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	headers := make([]any, len(s.columns))
	seen := make(map[string]int)
	for i, column := range s.columns {
		if err := stream.SetColWidth(i+1, i+1, column.width); err != nil {
			return err
		}
		label := column.label
		// Excel tables require unique, non-empty header labels.
		if label == "" {
			label = strconv.Itoa(i + 1)
		}
		original := label
		for seen[strings.ToLower(label)] > 0 {
			seen[strings.ToLower(original)]++
			label = fmt.Sprintf("%s (%d)", original, seen[strings.ToLower(original)])
		}
		seen[strings.ToLower(label)] = 1
		headers[i] = excelize.Cell{StyleID: s.book.styles[0]["header"], Value: label}
	}
	return stream.SetRow("A1", headers, excelize.RowOpts{Height: 32})
}

func (s *exportSheet) addRow(values ...any) error {
	if err := s.book.ctx.Err(); err != nil {
		return err
	}
	if len(values) != len(s.columns) {
		return fmt.Errorf("export row has %d values for %d columns", len(values), len(s.columns))
	}
	if s.row == s.maxRows {
		if err := s.finish(); err != nil {
			return err
		}
		if err := s.start(); err != nil {
			return err
		}
	}
	s.row++
	cells := make([]any, len(values))
	height := 24.0
	for i, value := range values {
		kind := s.columns[i].kind
		if kind == "" {
			kind = "text"
		}
		if stamp, ok := value.(time.Time); ok {
			if stamp.IsZero() {
				value = nil
			} else {
				value = stamp.UTC()
			}
		}
		if text, ok := value.(string); ok {
			if len(utf16.Encode([]rune(text))) > 32767 {
				return echo.NewHTTPError(http.StatusBadRequest, s.book.lang.T("exports.cellTooLong"))
			}
			lines := 0
			for _, line := range strings.Split(text, "\n") {
				lines += 1 + int(float64(len([]rune(line)))/s.columns[i].width)
			}
			height = max(height, min(90, float64(lines)*15))
		}
		// Values are literal text; user input never becomes an Excel formula.
		cells[i] = excelize.Cell{StyleID: s.book.styles[s.row%2][kind], Value: value}
	}
	return s.stream.SetRow(fmt.Sprintf("A%d", s.row), cells, excelize.RowOpts{Height: height})
}

func (s *exportSheet) finish() error {
	if s.stream == nil {
		return nil
	}
	if s.row > 1 {
		last, err := excelize.CoordinatesToCellName(len(s.columns), s.row)
		if err != nil {
			return err
		}
		if err := s.stream.AddTable(&excelize.Table{Range: "A1:" + last, Name: fmt.Sprintf("ExportTable%d", s.index), StyleName: "TableStyleMedium2"}); err != nil {
			return err
		}
	}
	if err := s.stream.Flush(); err != nil {
		return err
	}
	s.stream = nil
	return nil
}

func (b *exportWorkbook) finish() error {
	for _, sheet := range b.sheets {
		if err := sheet.finish(); err != nil {
			return err
		}
	}
	return b.ctx.Err()
}

func (b *exportWorkbook) download(c echo.Context, filename string) error {
	if err := b.finish(); err != nil {
		return err
	}
	// Complete the ZIP before committing HTTP headers. Database/writer errors
	// then produce an ordinary error response instead of a corrupt download.
	tmp, err := os.CreateTemp("", "listmonk-export-*.xlsx")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := b.file.WriteTo(tmp); err != nil {
		return err
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		return err
	}
	h := c.Response().Header()
	h.Set(echo.HeaderContentDisposition, mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	return c.Stream(http.StatusOK, excelContentType, tmp)
}

func exportJSONValue(value any) any {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		b, _ := json.MarshalIndent(value, "", "  ")
		return string(b)
	}
}

func exportTimestamp(value any) any {
	if text, ok := value.(string); ok {
		if stamp, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return stamp.UTC()
		}
	}
	return value
}
