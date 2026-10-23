package main

import (
	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/media"
	"github.com/labstack/echo/v4"
)

// An HTML URL is not an alternative way to attach media without the role gate.
// Core additionally checks the actual resource owner's send rights under lock.
func (a *App) requireBodyMediaPermission(c echo.Context, body string) error {
	root := ""
	if a.urlCfg != nil {
		root = a.urlCfg.RootURL
	}
	for _, ref := range media.BodyReferences([]byte(body)) {
		if _, _, protected, _ := media.ProtectedReference(ref.Source, root); protected {
			return requireLegacyPermission(auth.GetUser(c), auth.PermMediaGet)
		}
	}
	return nil
}
