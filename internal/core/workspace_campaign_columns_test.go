package core

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

// TestCampaignListColumnsCoverMappedFields guards the explicit list projection:
// a new db-tagged Campaign field backed by a campaigns column must be added to
// campaignListColumns, otherwise the API silently returns a zero value.
func TestCampaignListColumnsCoverMappedFields(t *testing.T) {
	columns := campaignListColumns(false)
	listed := map[string]struct{}{}
	for _, match := range regexp.MustCompile(`\bc\.([a-z_]+)`).FindAllStringSubmatch(columns, -1) {
		listed[match[1]] = struct{}{}
	}

	// db-tagged fields that are computed or joined by other queries and are not
	// columns of the campaigns table.
	computed := map[string]struct{}{
		"organization_name": {}, "organization_archived": {}, "media_folder_id": {},
		"customer_list_type": {}, "owner_username": {}, "owner_name": {},
		"unsent_count": {}, "reply_mailbox_email": {}, "owner_user_attribs": {},
		"template_name_fallback": {}, "template_body": {}, "archive_template_body": {},
		"media_id": {}, "delivery_total": {}, "delivery_sent": {},
		"delivery_pending": {}, "delivery_unavailable": {}, "campaign_id": {},
		"views": {}, "clicks": {}, "bounces": {}, "unsubscribes": {},
		"unsubscribe_clicks": {}, "unsubscribe_activity": {},
		"customer_lists": {}, "customer_pools": {}, "media": {}, "total": {},
	}

	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				walk(field.Type)
				continue
			}
			tag := strings.Split(field.Tag.Get("db"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			if _, ok := computed[tag]; ok {
				continue
			}
			if _, ok := listed[tag]; !ok {
				t.Errorf("campaignListColumns is missing mapped campaigns column %q", tag)
			}
		}
	}
	walk(reflect.TypeOf(models.Campaign{}))

	noBody := campaignListColumns(true)
	if regexp.MustCompile(`\bc\.body\b`).MatchString(noBody) || regexp.MustCompile(`\bc\.body_source\b`).MatchString(noBody) {
		t.Error("no_body projection still reads the stored body columns")
	}
	if !strings.Contains(noBody, "AS body,") || !strings.Contains(noBody, "AS body_source") {
		t.Error("no_body projection does not alias empty body columns")
	}
}
