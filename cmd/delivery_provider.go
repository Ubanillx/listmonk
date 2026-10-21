package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/bestedm"
	apptz "github.com/knadh/listmonk/internal/tz"
	"github.com/labstack/echo/v4"
)

const bestEDMSettingsKey = "integration.bestedm"

// The provider account is platform-wide. It must never be returned through
// models.Settings or exposed without an explicit provider permission.
type bestEDMConfig struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	UpdatedAt string `json:"updated_at"`
	TestedAt  string `json:"tested_at,omitempty"`
	TestError string `json:"test_error,omitempty"`
	Verified  bool   `json:"verified"`
}
type bestEDMAPI interface {
	Customer(context.Context, bestedm.Credentials) (bestedm.Row, error)
	Service(context.Context, bestedm.Credentials) (bestedm.Row, error)
	Statistics(context.Context, bestedm.Credentials, url.Values) ([]bestedm.Row, error)
	Failures(context.Context, bestedm.Credentials, url.Values) ([]bestedm.Row, error)
	Logs(context.Context, bestedm.Credentials, url.Values) ([]map[string]any, error)
}

func (a *App) bestEDM() bestEDMAPI {
	if a.bestEDMClient != nil {
		return a.bestEDMClient
	}
	return bestedm.New()
}
func requireBestEDMAdmin(c echo.Context) error {
	if !auth.GetUser(c).IsPlatformAdmin() {
		return auth.ErrPermDenied
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return nil
}
func requireBestEDMRead(c echo.Context) error {
	u := auth.GetUser(c)
	if !u.HasPerm(auth.PermDeliveryProviderGet) {
		return auth.ErrPermDenied
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return nil
}
func (a *App) loadBestEDMConfig(ctx context.Context) (bestEDMConfig, []byte, error) {
	var raw []byte
	var config bestEDMConfig
	err := a.db.GetContext(ctx, &raw, `SELECT value FROM settings WHERE key=$1`, bestEDMSettingsKey)
	if errors.Is(err, sql.ErrNoRows) {
		return config, nil, nil
	}
	if err != nil {
		return config, nil, echo.NewHTTPError(500, "无法读取服务商配置")
	}
	if json.Unmarshal(raw, &config) != nil {
		return config, nil, echo.NewHTTPError(500, "服务商配置格式错误")
	}
	return config, raw, nil
}
func bestEDMStatus(config bestEDMConfig) map[string]any {
	return map[string]any{"username": config.Username, "configured": config.Username != "" && config.Password != "", "has_password": config.Password != "", "updated_at": config.UpdatedAt, "tested_at": config.TestedAt, "test_error": config.TestError, "verified": config.Verified, "provider": "BestEDM"}
}
func (a *App) GetDeliveryProviderConfig(c echo.Context) error {
	if err := requireBestEDMRead(c); err != nil {
		return err
	}
	config, _, err := a.loadBestEDMConfig(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(200, okResp{bestEDMStatus(config)})
}
func (a *App) SaveDeliveryProviderConfig(c echo.Context) error {
	if err := requireBestEDMAdmin(c); err != nil {
		return err
	}
	var request struct {
		Username          string `json:"username"`
		Password          string `json:"password"`
		ExpectedUpdatedAt string `json:"expected_updated_at"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(400, "账号配置格式不正确")
	}
	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" || len(request.Username) > 320 || strings.ContainsAny(request.Username, ":\r\n\x00") || len(request.Password) > 2048 {
		return echo.NewHTTPError(400, "请输入有效的账号和密码")
	}
	ctx := c.Request().Context()
	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		return echo.NewHTTPError(500, "无法保存服务商配置")
	}
	defer tx.Rollback()
	// Serialize both initial creation and updates without holding a lock across
	// network requests; all provider account mutations take this same DB lock.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(728614821)`); err != nil {
		return echo.NewHTTPError(500, "无法锁定服务商配置")
	}
	var raw []byte
	var old bestEDMConfig
	err = tx.GetContext(ctx, &raw, `SELECT value FROM settings WHERE key=$1 FOR UPDATE`, bestEDMSettingsKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return echo.NewHTTPError(500, "无法读取服务商配置")
	}
	if len(raw) > 0 && json.Unmarshal(raw, &old) != nil {
		return echo.NewHTTPError(500, "服务商配置格式错误")
	}
	if old.UpdatedAt != request.ExpectedUpdatedAt {
		return echo.NewHTTPError(409, "配置已被其他管理员修改，请刷新后重试")
	}
	if request.Password == "" {
		if old.Username != request.Username || old.Password == "" {
			return echo.NewHTTPError(400, "首次配置或更换账号时必须填写密码")
		}
		request.Password = old.Password
	}
	config := bestEDMConfig{Username: request.Username, Password: request.Password, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	b, _ := json.Marshal(config)
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES($1,$2,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()`, bestEDMSettingsKey, b); err != nil {
		return echo.NewHTTPError(500, "无法保存服务商配置")
	}
	if err = tx.Commit(); err != nil {
		return echo.NewHTTPError(500, "无法保存服务商配置")
	}
	setAuditObjectDetails(c, map[string]any{"provider": "BestEDM"})
	return c.JSON(200, okResp{bestEDMStatus(config)})
}
func providerHTTPError(err error) error {
	var pe *bestedm.Error
	if errors.As(err, &pe) {
		return echo.NewHTTPError(http.StatusBadGateway, pe.Message+"（"+pe.Code+"）")
	}
	return echo.NewHTTPError(502, "服务商查询失败，请稍后重试")
}
func (a *App) TestDeliveryProvider(c echo.Context) error {
	if err := requireBestEDMAdmin(c); err != nil {
		return err
	}
	cfg, raw, err := a.loadBestEDMConfig(c.Request().Context())
	if err != nil {
		return err
	}
	if cfg.Password == "" {
		return echo.NewHTTPError(400, "请先保存服务商账号密码")
	}
	throttleKey := fmt.Sprintf("bestedm:%d", auth.GetUser(c).ID)
	if !a.throttle.Allow(throttleKey) {
		return echo.NewHTTPError(429, "连接尝试过于频繁，请稍后重试")
	}
	credentials := bestedm.Credentials{Username: cfg.Username, Password: cfg.Password}
	_, err = a.bestEDM().Customer(c.Request().Context(), credentials)
	if err == nil {
		_, err = a.bestEDM().Service(c.Request().Context(), credentials)
	}
	cfg.TestedAt = time.Now().UTC().Format(time.RFC3339)
	cfg.Verified = err == nil
	cfg.TestError = ""
	if err != nil {
		a.throttle.Failure(throttleKey)
		cfg.TestError = providerHTTPError(err).(*echo.HTTPError).Message.(string)
		setAuditOutcome(c, "failed", "provider_connection_failed")
	} else {
		a.throttle.Success(throttleKey)
	}
	b, _ := json.Marshal(cfg)
	result, dbErr := a.db.ExecContext(c.Request().Context(), `UPDATE settings SET value=$2,updated_at=NOW() WHERE key=$1 AND value=$3::jsonb`, bestEDMSettingsKey, b, raw)
	if dbErr != nil {
		return echo.NewHTTPError(500, "无法保存连接测试结果")
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return echo.NewHTTPError(409, "账号配置已变更，请重新测试")
	}
	// Persist failures and return their safe detail, so a reload does not erase
	// the only explanation of an unsuccessful connection.
	return c.JSON(200, okResp{bestEDMStatus(cfg)})
}
func (a *App) deliveryProviderCredentials(c echo.Context) (bestedm.Credentials, error) {
	if a.bestEDMCreds != nil {
		return *a.bestEDMCreds, nil
	}
	cfg, _, err := a.loadBestEDMConfig(c.Request().Context())
	if err != nil {
		return bestedm.Credentials{}, err
	}
	if cfg.Username == "" || cfg.Password == "" {
		return bestedm.Credentials{}, echo.NewHTTPError(400, "请先在账号设置中保存服务商账号密码")
	}
	return bestedm.Credentials{Username: cfg.Username, Password: cfg.Password}, nil
}
func providerResponse(c echo.Context, data any, source string, warning string) error {
	return c.JSON(200, okResp{map[string]any{"results": data, "source": source, "fetched_at": apptz.Now().Format(time.RFC3339), "warning": warning}})
}
func (a *App) GetDeliveryProviderAccount(c echo.Context) error {
	if err := requireBestEDMRead(c); err != nil {
		return err
	}
	// Reports have one data source; a stale sample-mode link must not silently
	// return live data under the old contract.
	if c.QueryParam("mode") != "" {
		return echo.NewHTTPError(400, "报表不支持数据模式参数")
	}
	cred, err := a.deliveryProviderCredentials(c)
	if err != nil {
		return err
	}
	customer, err := a.bestEDM().Customer(c.Request().Context(), cred)
	if err != nil {
		return providerHTTPError(err)
	}
	service, err := a.bestEDM().Service(c.Request().Context(), cred)
	warning := ""
	if err != nil {
		warning = "账号资料已取得，但服务额度查询失败，请稍后刷新"
	}
	return providerResponse(c, map[string]any{"customer": customer, "service": service}, "live", warning)
}

func validateBestEDMQuery(kind string, q url.Values) error {
	allowed := map[string]bool{"lang": true}
	keys := []string{"date", "date_end", "ident", "id"}
	if kind == "failures" {
		keys = []string{"date", "date_end", "task_id", "task_ident", "type", "search", "group_by"}
	}
	if kind == "logs" {
		keys = []string{"date", "mail_to", "task_ident"}
	}
	for _, k := range keys {
		allowed[k] = true
	}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || len(values[0]) > 320 || strings.ContainsAny(values[0], "\r\n\x00") {
			return fmt.Errorf("查询参数不正确")
		}
	}
	for _, key := range []string{"date", "date_end"} {
		value := q.Get(key)
		if value != "" {
			parsed, err := time.Parse("2006-01-02", value)
			if err != nil || parsed.Format("2006-01-02") != value {
				return fmt.Errorf("日期必须使用 YYYY-MM-DD 格式")
			}
		}
	}
	if q.Get("date") != "" && q.Get("date_end") != "" {
		start, _ := time.Parse("2006-01-02", q.Get("date"))
		end, _ := time.Parse("2006-01-02", q.Get("date_end"))
		if end.Before(start) {
			return fmt.Errorf("结束日期不能早于开始日期")
		}
		if end.Sub(start) > 366*24*time.Hour {
			return fmt.Errorf("单次最多查询 366 天，请缩小范围")
		}
	}
	for _, key := range []string{"id", "task_id"} {
		if value := q.Get(key); value != "" {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id < 1 {
				return fmt.Errorf("任务 ID 必须是正整数")
			}
		}
	}
	switch kind {
	case "statistics":
		if q.Get("ident") != "" && q.Get("id") != "" {
			return fmt.Errorf("任务 ID 和批次号只需选择一项")
		}
		if q.Get("date") == "" && q.Get("date_end") == "" && q.Get("ident") == "" && q.Get("id") == "" {
			return fmt.Errorf("请指定日期或任务")
		}
	case "failures":
		if group := q.Get("group_by"); group != "" && group != "email" {
			return fmt.Errorf("不支持此汇总方式")
		}
		typ := q.Get("type")
		n, err := strconv.Atoi(typ)
		if err != nil || (n != -1 && (n < 1 || n > 10)) || strconv.Itoa(n) != typ {
			return fmt.Errorf("请选择有效的失败类型")
		}
		if q.Get("task_id") != "" && q.Get("task_ident") != "" {
			return fmt.Errorf("统计任务 ID 和批次号只需选择一项")
		}
		if (n == 1 || n == 2 || n == 3) && q.Get("task_id") == "" && q.Get("task_ident") == "" {
			return fmt.Errorf("该失败类型按文档要求必须填写批次号或统计任务 ID")
		}
		if q.Get("date") == "" && q.Get("date_end") == "" && q.Get("task_id") == "" && q.Get("task_ident") == "" {
			return fmt.Errorf("请指定日期或任务")
		}
	case "logs":
		if q.Get("date") == "" {
			return fmt.Errorf("请选择查询日期")
		}
		value := q.Get("mail_to")
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value {
			return fmt.Errorf("请输入完整的收件邮箱地址")
		}
	}
	return nil
}
func (a *App) QueryDeliveryProvider(c echo.Context) error {
	if err := requireBestEDMRead(c); err != nil {
		return err
	}
	kind := c.Param("kind")
	if format := c.Param("format"); format != "" && (format != "export" || kind != "failures") {
		return echo.NewHTTPError(404, "接口不存在")
	}
	if kind != "statistics" && kind != "failures" && kind != "logs" {
		return echo.NewHTTPError(404, "接口不存在")
	}
	q := url.Values{}
	for key, values := range c.QueryParams() {
		q[key] = append([]string(nil), values...)
	}
	if err := validateBestEDMQuery(kind, q); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	source := "live"
	q.Del("lang")
	// Email search is local to this console; never forward it to BestEDM.
	search := strings.ToLower(strings.TrimSpace(q.Get("search")))
	q.Del("search")
	groupByEmail := q.Get("group_by") == "email"
	q.Del("group_by")
	if kind == "failures" {
		if q.Get("date") != "" && q.Get("date_end") == "" {
			q.Set("date_end", q.Get("date"))
		}
		if q.Get("date_end") != "" && q.Get("date") == "" {
			q.Set("date", q.Get("date_end"))
		}
	}
	var rows []bestedm.Row
	var logs []map[string]any
	var err error
	cred, err := a.deliveryProviderCredentials(c)
	if err != nil {
		return err
	}
	switch kind {
	case "statistics":
		rows, err = a.bestEDM().Statistics(c.Request().Context(), cred, q)
	case "failures":
		rows, err = a.bestEDM().Failures(c.Request().Context(), cred, q)
	case "logs":
		logs, err = a.bestEDM().Logs(c.Request().Context(), cred, q)
	}
	if err != nil {
		return providerHTTPError(err)
	}
	if kind == "failures" && search != "" {
		filtered := make([]bestedm.Row, 0, len(rows))
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row["email"]), search) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	var summaries []providerFailureSummary
	if groupByEmail {
		summaries = summarizeProviderFailures(rows)
		// Attach the workspace match per address so one report answers both
		// "why did it fail" and "which customer is this". Excel export keeps the
		// report-only contract and does no matching work.
		if c.Param("format") == "" {
			if access, accessErr := a.workspaceAccess(c); accessErr == nil {
				a.providerFailureMatches(c, access, summaries)
			}
		}
	}
	if c.Param("format") == "export" {
		if kind != "failures" {
			return echo.NewHTTPError(404, "此接口不支持导出")
		}
		book, e := a.newExportWorkbook(c)
		if e != nil {
			return e
		}
		defer book.file.Close()
		columns := []exportColumn{book.column("customers.email", 38, "")}
		if groupByEmail {
			columns = append(columns, book.column("deliveryProvider.errorCount", 16, "0"))
		}
		columns = append(columns, book.column("deliveryProvider.errorType", 24, ""), book.column("deliveryProvider.errorDetail", 45, ""), book.column("deliveryProvider.source", 20, ""))
		sheet, e := book.addSheet("deliveryProvider.failures", columns)
		if e != nil {
			return e
		}
		if groupByEmail {
			for _, row := range summaries {
				types, details := make([]string, 0, len(row.Errors)), make([]string, 0, len(row.Errors))
				for _, cause := range row.Errors {
					types = append(types, fmt.Sprintf("%s (%d)", cause.Type, cause.Count))
					details = append(details, fmt.Sprintf("%s (%d)", cause.Detail, cause.Count))
				}
				if e = sheet.addRow(row.Email, row.Count, strings.Join(types, "\n"), strings.Join(details, "\n"), source); e != nil {
					return e
				}
			}
		} else {
			for _, row := range rows {
				if e = sheet.addRow(row["email"], row["error_type"], row["error_type_dec"], source); e != nil {
					return e
				}
			}
		}
		return book.download(c, "bestedm-failures-"+source+".xlsx")
	}
	if kind == "logs" {
		return providerResponse(c, logs, source, "日志查询受服务商每日次数限制，请按需使用")
	}
	if groupByEmail {
		return providerResponse(c, summaries, source, "")
	}
	return providerResponse(c, rows, source, "")
}
