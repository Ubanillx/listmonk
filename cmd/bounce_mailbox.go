package main

import (
	"net/http"
	"strings"

	"github.com/knadh/listmonk/internal/bounce/mailbox"
	"github.com/labstack/echo/v4"
)

type bounceMailboxTestRequest struct {
	UUID          string `json:"uuid"`
	Type          string `json:"type"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	AuthProtocol  string `json:"auth_protocol"`
	TLSEnabled    bool   `json:"tls_enabled"`
	StartTLS      bool   `json:"starttls"`
	TLSSkipVerify bool   `json:"tls_skip_verify"`
}

// TestBounceMailbox tests the unsaved form without updating settings or events.
func (a *App) TestBounceMailbox(c echo.Context) error {
	var req bounceMailboxTestRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid mailbox configuration")
	}
	req.Host = strings.TrimSpace(req.Host)
	if (req.Type != "" && req.Type != "pop") || req.Host == "" || req.Port < 1 || req.Port > 65535 || (req.AuthProtocol != "none" && req.AuthProtocol != "userpass") || (req.StartTLS && req.TLSEnabled) {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid host, port, authentication or TLS mode")
	}
	if req.AuthProtocol != "none" {
		if req.Password == "" || strings.Trim(req.Password, pwdMask) == "" {
			cur, err := a.core.GetSettings()
			if err != nil {
				return err
			}
			req.Password = ""
			for _, box := range cur.BounceBoxes {
				if req.UUID != "" && box.UUID == req.UUID {
					req.Password = box.Password
					break
				}
			}
		}
		if req.Username == "" || req.Password == "" || strings.ContainsAny(req.Username+req.Password, "\r\n") {
			return echo.NewHTTPError(http.StatusBadRequest, "Enter the mailbox username and password")
		}
	}
	result := mailbox.Test(c.Request().Context(), mailbox.Opt{Host: req.Host, Port: req.Port, Username: req.Username, Password: req.Password, AuthProtocol: req.AuthProtocol, TLSEnabled: req.TLSEnabled, StartTLS: req.StartTLS, TLSSkipVerify: req.TLSSkipVerify})
	for i, step := range result.Steps {
		if req.Password != "" {
			result.Steps[i].Detail = strings.ReplaceAll(step.Detail, req.Password, "[redacted]")
		}
		if step.Status == "failed" {
			a.log.Printf("bounce mailbox test failed at %s: %s", step.Name, result.Steps[i].Detail)
		}
	}
	return c.JSON(http.StatusOK, okResp{result})
}
