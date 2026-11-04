package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"

	auditlog "github.com/knadh/listmonk/internal/audit"
	"github.com/knadh/listmonk/internal/bestedm"
	"github.com/knadh/listmonk/internal/providerautomation"
	apptz "github.com/knadh/listmonk/internal/tz"
	"github.com/labstack/echo/v4"
)

func (a *App) providerAutomation() *providerautomation.Service {
	return &providerautomation.Service{DB: a.db, Now: apptz.Now,
		Fetch: func(ctx context.Context, q url.Values) ([]bestedm.Row, error) {
			config, _, err := a.loadBestEDMConfig(ctx)
			if err != nil {
				return nil, err
			}
			if config.Username == "" || config.Password == "" {
				return nil, &bestedm.Error{Code: "not_configured", Message: "请先配置发件服务账号"}
			}
			return a.bestEDM().Failures(ctx, bestedm.Credentials{Username: config.Username, Password: config.Password}, q)
		},
		Audit: func(scope, result string, counts providerautomation.Counts) {
			if a.audit == nil {
				return
			}
			err := a.audit.Record(context.Background(), auditlog.Event{
				ActorType: "system", Action: "delivery_provider.auto_blocklist", ObjectType: "delivery_provider", ObjectID: scope, Result: result,
				Metadata: map[string]any{"scope": scope, "changed_count": counts.ToBlock, "unmatched_count": counts.Unmatched},
			})
			if err != nil && a.log != nil {
				a.log.Print("error recording provider automation audit")
			}
		},
	}
}

func automationHTTPError(err error) error {
	code := 500
	if errors.Is(err, providerautomation.ErrBusy) || errors.Is(err, providerautomation.ErrConflict) || errors.Is(err, providerautomation.ErrPreview) {
		code = 409
	}
	var pe *bestedm.Error
	if errors.As(err, &pe) {
		code = 502
	}
	return echo.NewHTTPError(code, providerautomation.SafeError(err))
}

func (a *App) GetProviderAutomation(c echo.Context) error {
	if err := requireBestEDMAdmin(c); err != nil {
		return err
	}
	service := a.providerAutomation()
	result := []map[string]any{}
	for _, scope := range []string{"private", "pool"} {
		rule, err := service.Rule(c.Request().Context(), scope)
		if err != nil {
			return automationHTTPError(err)
		}
		runs, err := service.Runs(c.Request().Context(), scope)
		if err != nil {
			return automationHTTPError(err)
		}
		result = append(result, map[string]any{"rule": rule, "runs": runs})
	}
	return c.JSON(200, okResp{map[string]any{"scopes": result, "timezone": apptz.Name()}})
}

func (a *App) ChangeProviderAutomation(c echo.Context) error {
	if err := requireBestEDMAdmin(c); err != nil {
		return err
	}
	scope := c.Param("scope")
	if !providerautomation.ValidScope(scope) {
		return echo.NewHTTPError(400, "invalid scope")
	}
	var request struct {
		Version   int64                      `json:"version"`
		PreviewID int64                      `json:"preview_id"`
		Config    *providerautomation.Config `json:"config"`
	}
	if err := c.Bind(&request); err != nil {
		return echo.NewHTTPError(400, "invalid request")
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 45*time.Second)
	defer cancel()
	service := a.providerAutomation()
	if c.Request().Method == "PUT" || c.Param("action") == "preview" || request.Config != nil {
		if request.Config == nil {
			return echo.NewHTTPError(400, "config is required")
		}
		if err := request.Config.Validate(); err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
	}
	if c.Request().Method == "PUT" {
		rule, err := service.Save(ctx, scope, request.Version, request.PreviewID, *request.Config)
		if err != nil {
			return automationHTTPError(err)
		}
		setAuditMetadata(c, map[string]any{"scope": scope, "enabled": rule.Config.Enabled, "version": rule.Version})
		return c.JSON(200, okResp{rule})
	}
	var run providerautomation.Run
	var err error
	switch c.Param("action") {
	case "preview":
		run, err = service.Process(ctx, scope, request.Version, 0, request.Config, false)
	case "run":
		run, err = service.Execute(ctx, scope, request.Version, request.PreviewID, request.Config)
	default:
		return echo.NewHTTPError(404, "unknown action")
	}
	if err != nil {
		return automationHTTPError(err)
	}
	items, err := service.Items(ctx, scope, run.ID, 1)
	if err != nil {
		return automationHTTPError(err)
	}
	setAuditMetadata(c, map[string]any{"scope": scope, "run_id": run.ID, "changed_count": run.Counts.ToBlock})
	return c.JSON(200, okResp{map[string]any{"run": run, "items": items}})
}

func (a *App) GetProviderAutomationItems(c echo.Context) error {
	if err := requireBestEDMAdmin(c); err != nil {
		return err
	}
	scope := c.Param("scope")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	page := 1
	if p := c.QueryParam("page"); p != "" {
		page, err = strconv.Atoi(p)
	}
	if !providerautomation.ValidScope(scope) || id < 1 || page < 1 || page > 100000 || err != nil {
		return echo.NewHTTPError(400, "invalid detail query")
	}
	items, err := a.providerAutomation().Items(c.Request().Context(), scope, id, page)
	if err != nil {
		return automationHTTPError(err)
	}
	return c.JSON(200, okResp{items})
}

func (a *App) runProviderAutomation(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		for _, scope := range []string{"private", "pool"} {
			work, cancel := context.WithTimeout(ctx, 45*time.Second)
			_, err := a.providerAutomation().Process(work, scope, 0, 0, nil, true)
			cancel()
			if err != nil && !errors.Is(err, providerautomation.ErrBusy) && ctx.Err() == nil {
				a.log.Printf("provider automation %s: %s", scope, providerautomation.SafeError(err))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
