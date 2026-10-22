package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knadh/listmonk/internal/auth"
	"github.com/knadh/listmonk/internal/media"
	"github.com/knadh/paginator"
	"github.com/labstack/echo/v4"
)

type catalogTestMediaStore struct{}

func (catalogTestMediaStore) Put(string, string, io.ReadSeeker) (string, error) { return "", nil }
func (catalogTestMediaStore) Delete(string) error                               { return nil }
func (catalogTestMediaStore) GetURL(string) string                              { return "" }
func (catalogTestMediaStore) GetBlob(string) ([]byte, error)                    { return []byte("catalog file"), nil }

func TestPlatformMediaCatalogGroupsWithoutWideningWorkspaceMedia(t *testing.T) {
	a := newOrgPoolAllocationTestApp(t)
	seedOrgPoolAllocationFixtures(t, a.db)
	a.cfg = &Config{}
	a.cfg.MediaUpload.Provider = "filesystem"
	a.media = catalogTestMediaStore{}
	a.pg = paginator.New(paginator.Opt{DefaultPerPage: 20, MaxPerPage: 50, PageParam: "page", PerPageParam: "per_page", AllowAll: true})
	var folderID int
	if err := a.db.Get(&folderID, `INSERT INTO media_folders(name,organization_id,owner_user_id,visibility)
		VALUES('Private folder',$1,$2,'private') RETURNING id`, orgPoolAllocationTestOtherOrgID, orgPoolAllocationTestOutsiderUser); err != nil {
		t.Fatal(err)
	}
	var folderMediaID, personalMediaID int
	if err := a.db.Get(&folderMediaID, `INSERT INTO media(uuid,provider,filename,thumb,folder_id,organization_id,owner_user_id,visibility)
		VALUES(gen_random_uuid(),'filesystem','foreign.png','',$1,$2,$3,'private') RETURNING id`, folderID, orgPoolAllocationTestOtherOrgID, orgPoolAllocationTestOutsiderUser); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Get(&personalMediaID, `INSERT INTO media(uuid,provider,filename,thumb,owner_user_id,visibility)
		VALUES(gen_random_uuid(),'filesystem','personal.png','',$1,'private') RETURNING id`, orgPoolAllocationTestMemberUser); err != nil {
		t.Fatal(err)
	}
	var pendingMediaID int
	if err := a.db.Get(&pendingMediaID, `INSERT INTO media(uuid,provider,filename,thumb,organization_id,original_owner_user_id,visibility)
		VALUES(gen_random_uuid(),'filesystem','pending.png','',$1,$2,'private') RETURNING id`, orgPoolAllocationTestOtherOrgID, orgPoolAllocationTestOutsiderUser); err != nil {
		t.Fatal(err)
	}
	admin := auth.User{Base: auth.Base{ID: orgPoolAllocationTestAdminUser}, UserRoleID: auth.SuperAdminRoleID}
	request := func(url string, user auth.User) (echo.Context, *httptest.ResponseRecorder) {
		t.Helper()
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, url, nil), rec)
		c.Set(auth.UserHTTPCtxKey, user)
		return c, rec
	}
	ordinary, ordinaryRecorder := request("/api/media?per_page=all", admin)
	if err := a.GetAllMedia(ordinary); err != nil {
		t.Fatal(err)
	}
	var normal struct {
		Data struct {
			Results []media.Media `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(ordinaryRecorder.Body.Bytes(), &normal); err != nil {
		t.Fatal(err)
	}
	if len(normal.Data.Results) != 0 {
		t.Fatalf("ordinary personal workspace leaked other media: %+v", normal.Data.Results)
	}

	platform, recorder := request("/api/media?scope=all_owners&per_page=all", admin)
	if err := a.GetAllMedia(platform); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Data struct {
			Results []media.Media `json:"results"`
			Total   int           `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Data.Total != 3 || len(page.Data.Results) != 3 {
		t.Fatalf("platform media = %+v", page.Data)
	}
	var foreignMedia media.Media
	for _, item := range page.Data.Results {
		if item.ID == folderMediaID {
			foreignMedia = item
		}
	}
	if foreignMedia.FolderName != "Private folder" || foreignMedia.OrganizationName != "Pool Allocation Other" {
		t.Fatalf("foreign folder metadata = %+v", foreignMedia)
	}
	if got := foreignMedia.URL; got != fmt.Sprintf("/api/media/file/%d/foreign.png?scope=all_owners", folderMediaID) {
		t.Fatalf("catalog URL = %q", got)
	}
	filtered, filteredRecorder := request(fmt.Sprintf("/api/media?scope=all_owners&organization_id=0&owner_user_id=%d", orgPoolAllocationTestMemberUser), admin)
	if err := a.GetAllMedia(filtered); err != nil {
		t.Fatal(err)
	}
	var personalPage struct {
		Data struct {
			Results []media.Media `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(filteredRecorder.Body.Bytes(), &personalPage); err != nil || len(personalPage.Data.Results) != 1 || personalPage.Data.Results[0].ID != personalMediaID {
		t.Fatalf("personal group = %+v err=%v", personalPage, err)
	}
	access, err := a.workspaceAccess(platform)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := a.core.QueryPlatformMediaCategories(access, "filesystem")
	if err != nil || len(groups) != 2 {
		t.Fatalf("categories = %+v err=%v", groups, err)
	}
	var otherGroup media.OwnerCategory
	for _, group := range groups {
		if group.OrganizationID == orgPoolAllocationTestOtherOrgID {
			otherGroup = group
		}
	}
	if otherGroup.OwnerUserID != orgPoolAllocationTestOutsiderUser || otherGroup.MediaCount != 2 {
		t.Fatalf("pending transfer was not grouped with its original owner: %+v", otherGroup)
	}
	filteredPending, _, err := a.core.QueryPlatformMedia(access, "filesystem", "pending", orgPoolAllocationTestOtherOrgID, otherGroup.OwnerUserID, 0, 20)
	if err != nil || len(filteredPending) != 1 || filteredPending[0].ID != pendingMediaID {
		t.Fatalf("pending transfer category filter = %+v err=%v", filteredPending, err)
	}
	if _, err := a.core.GetWorkspaceMediaByID(access, folderMediaID); err == nil {
		t.Fatal("ordinary media URL crossed private folder boundary")
	}
	if _, err := a.core.GetPlatformMediaByID(access, "filesystem", folderMediaID); err != nil {
		t.Fatalf("platform media detail: %v", err)
	}
	fileURL := fmt.Sprintf("/api/media/file/%d/foreign.png", folderMediaID)
	ordinaryFile, _ := request(fileURL, admin)
	ordinaryFile.SetParamNames("id", "filename")
	ordinaryFile.SetParamValues(fmt.Sprint(folderMediaID), "foreign.png")
	if err := a.ServeMediaFileByID(ordinaryFile); err == nil {
		t.Fatal("ordinary media URL bypassed folder boundary")
	}
	platformFile, fileRecorder := request(fileURL+"?scope=all_owners", admin)
	platformFile.SetParamNames("id", "filename")
	platformFile.SetParamValues(fmt.Sprint(folderMediaID), "foreign.png")
	if err := a.ServeMediaFileByID(platformFile); err != nil || fileRecorder.Code != http.StatusOK {
		t.Fatalf("platform file read: status=%d err=%v", fileRecorder.Code, err)
	}
	unauthorized, _ := request("/api/media?scope=all_owners", auth.User{Base: auth.Base{ID: orgPoolAllocationTestMemberUser}, UserRoleID: 2})
	if ok, err := requirePlatformMediaCatalog(unauthorized); ok || err == nil {
		t.Fatal("ordinary account obtained platform catalog")
	}
	key, _ := request("/api/media?scope=all_owners", admin)
	key.Set(auth.IntegrationTokenHTTPCtxKey, auth.IntegrationToken{Kind: auth.IntegrationTokenKindPersonal})
	if ok, err := requirePlatformMediaCatalog(key); ok || err == nil {
		t.Fatal("personal key obtained platform catalog")
	}
}
