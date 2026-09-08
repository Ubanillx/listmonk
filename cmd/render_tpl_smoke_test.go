package main

import (
	"os"
	"strings"
	"testing"

	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/stuffbin"
)

func renderTemplateSmoke(t *testing.T, name string, data any) string {
	t.Helper()

	langB, err := os.ReadFile("../i18n/en.json")
	if err != nil {
		t.Fatalf("reading i18n/en.json: %v", err)
	}
	i, err := i18n.New(langB)
	if err != nil {
		t.Fatalf("initializing i18n: %v", err)
	}
	urlCfg := &UrlConfig{RootURL: "http://localhost:9000"}

	fs, err := stuffbin.NewLocalFS("/", "../static/public:/public")
	if err != nil {
		t.Fatalf("building local FS: %v", err)
	}
	tpls, err := stuffbin.ParseTemplatesGlob(initTplFuncs(i, urlCfg), fs, "/public/templates/*.html")
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}

	var out strings.Builder
	if err := tpls.ExecuteTemplate(&out, name, tplData{
		SiteName:     "listmonk",
		RootURL:      urlCfg.RootURL,
		AssetVersion: "test",
		Data:         data,
		L:            i,
	}); err != nil {
		t.Fatalf("rendering %s: %v", name, err)
	}
	return out.String()
}

func TestSelectWorkspaceTemplateSmoke(t *testing.T) {
	// Multiple spaces: personal + two organizations.
	multi := selectWorkspaceTpl{
		Title:   "Select workspace",
		NextURI: "/admin/campaigns",
		Spaces: []selectWorkspaceEntry{
			{Personal: true, Name: "Personal space"},
			{OrganizationID: 7, Name: "Acme <Co>", Role: "manager"},
			{OrganizationID: 9, Name: "Globex"},
		},
	}
	out := renderTemplateSmoke(t, "admin-select-workspace", multi)
	for _, want := range []string{
		"app-shell", "data-org=\"0\"", "data-org=\"7\"", "data-org=\"9\"",
		"Acme &lt;Co&gt;", "listmonk.workspace.organizationId",
		// The invite-code join help is permanently shown under the picker.
		"join-form", "join-code", "/api/organizations/join",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("multi-space render missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "AutoOrgID") || strings.Contains(out, "{{") {
		t.Errorf("unrendered template artifacts in multi-space render\n%s", out)
	}

	// Single space auto-enter into an organization.
	auto := selectWorkspaceTpl{
		Title:     "Select workspace",
		NextURI:   "/admin",
		AutoEnter: true,
		AutoOrgID: 7,
		AutoName:  "Acme",
	}
	out = renderTemplateSmoke(t, "admin-select-workspace", auto)
	for _, want := range []string{"enterWorkspace(", "localStorage.setItem", `"/admin"`} {
		if !strings.Contains(out, want) {
			t.Errorf("auto-enter render missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, `\"`) {
		t.Errorf("auto-enter render contains double-escaped JS string\n%s", out)
	}
	// The transient auto-enter page must not carry the join form.
	if strings.Contains(out, "join-form") {
		t.Errorf("auto-enter render must not include the join-help form\n%s", out)
	}

	// Single space auto-enter into the personal workspace.
	autoPersonal := selectWorkspaceTpl{
		Title:     "Select workspace",
		NextURI:   "/admin",
		AutoEnter: true,
		AutoName:  "Personal space",
	}
	out = renderTemplateSmoke(t, "admin-select-workspace", autoPersonal)
	if !strings.Contains(out, "enterWorkspace( 0") && !strings.Contains(out, "enterWorkspace(0") {
		t.Errorf("personal auto-enter render missing enterWorkspace(0)\n%s", out)
	}

	// No accessible space.
	blocked := selectWorkspaceTpl{
		Title: "Select workspace",
		Error: "No accessible workspace",
	}
	out = renderTemplateSmoke(t, "admin-select-workspace", blocked)
	for _, want := range []string{"No accessible workspace", "/admin/logout",
		// The blocked user still gets the invite-code join help.
		"join-form", "join-code", "/api/organizations/join",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("blocked render missing %q\n%s", want, out)
		}
	}
}

func TestLoginTemplateSmoke(t *testing.T) {
	// The login page must keep the classic light shell unchanged (it was
	// restored to the original header/footer layout).
	login := loginTpl{Title: "Login", PasswordEnabled: true, NextURI: "/admin/select-workspace?next=%2Fadmin", Nonce: "abc"}
	out := renderTemplateSmoke(t, "admin-login", login)
	for _, want := range []string{`action="/admin/login"`, "select-workspace", "container wrap"} {
		if !strings.Contains(out, want) {
			t.Errorf("login render missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "app-shell") {
		t.Errorf("login page must keep the classic shell, got fullscreen shell\n%s", out)
	}

	// The 2FA page keeps the classic shell as well.
	twofa := twofaTpl{Title: "2FA", Token: "tok", NextURI: "/admin"}
	out = renderTemplateSmoke(t, "admin-twofa", twofa)
	if strings.Contains(out, "app-shell") || !strings.Contains(out, "totp_code") {
		t.Errorf("2FA render must keep classic shell with the form\n%s", out)
	}
}
