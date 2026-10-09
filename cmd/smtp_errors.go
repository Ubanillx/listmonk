package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/labstack/echo/v4"
)

// smtpTestError explains the otherwise bare deadline returned by smtppool's
// implicit TLS dial. That deadline covers TCP connection and TLS handshake;
// SMTP authentication has not started on that failed connection.
func (a *App) smtpTestError(err error, server email.Server) error {
	message := err.Error()
	if server.TLSType == "TLS" && errors.Is(err, context.DeadlineExceeded) {
		wait := server.PoolWaitTimeout
		if wait < time.Second {
			// Match smtppool.New's normalization of the dial timeout.
			wait = 2 * time.Second
		}
		message = a.i18n.Ts("settings.smtp.tlsConnectionTimeout",
			"address", net.JoinHostPort(server.Host, strconv.Itoa(server.Port)),
			"timeout", wait.String())
	}
	return echo.NewHTTPError(http.StatusInternalServerError, message).SetInternal(err)
}
