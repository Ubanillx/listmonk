// media-audit inspects a read-only database inventory. It never connects to a
// database or sends mail. Optional SQL repairs only unambiguous, same-owner,
// root-folder references and revalidates the complete body under row locks.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/knadh/listmonk/internal/media"
)

type association struct {
	ID       *int   `json:"id"`
	Filename string `json:"filename"`
}
type resource struct {
	ID           int           `json:"id"`
	Owner        int           `json:"owner"`
	Organization *int          `json:"organization"`
	Body         string        `json:"body"`
	Status       string        `json:"status"`
	Sent         int           `json:"sent"`
	ContentType  string        `json:"content_type"`
	TemplateID   *int          `json:"template_id"`
	Media        []association `json:"media"`
}
type storedMedia struct {
	ID           int     `json:"id"`
	Filename     string  `json:"filename"`
	Owner        int     `json:"owner"`
	Organization *int    `json:"organization"`
	Visibility   string  `json:"visibility"`
	Pending      *string `json:"pending"`
	Folder       *int    `json:"folder"`
}
type inventory struct {
	Campaigns []resource    `json:"campaigns"`
	Templates []resource    `json:"templates"`
	Media     []storedMedia `json:"media"`
}
type finding struct {
	CampaignID int    `json:"campaign_id"`
	Status     string `json:"status"`
	Sent       int    `json:"sent"`
	Source     string `json:"source"`
	MediaID    int    `json:"media_id,omitempty"`
	Problem    string `json:"problem"`
}

func sameOrg(a, b *int) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func contains(refs []association, id int) bool {
	for _, r := range refs {
		if r.ID != nil && *r.ID == id {
			return true
		}
	}
	return false
}
func main() {
	input := flag.String("inventory", "", "JSON inventory containing campaigns, templates, media")
	output := flag.String("out", "", "JSON audit report")
	repair := flag.String("repair-sql", "", "optional conservative SQL backfill; review before executing")
	root := flag.String("root-url", "https://listmonk.topmax.cn", "application root URL")
	flag.Parse()
	if err := run(*input, *output, *repair, *root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(input, output, repair, root string) error {
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var inv inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return err
	}
	templates := map[int]resource{}
	for _, t := range inv.Templates {
		templates[t.ID] = t
	}
	byID := map[int]storedMedia{}
	for _, m := range inv.Media {
		byID[m.ID] = m
	}
	var findings []finding
	external := map[string]bool{}
	var sql strings.Builder
	sql.WriteString("BEGIN;\nSET LOCAL lock_timeout='5s';\nDO $repair$ BEGIN\nPERFORM 1 FROM campaigns WHERE id=110 AND status='paused' FOR UPDATE;\nIF NOT FOUND THEN RAISE EXCEPTION 'campaign 110 must remain paused'; END IF;\n")
	for _, c := range inv.Campaigns {
		if c.ContentType == "plain" {
			continue
		}
		var t resource
		if c.TemplateID != nil && c.ContentType != "visual" {
			t = templates[*c.TemplateID]
		}
		for _, ref := range media.BodyReferences([]byte(c.Body + "\n" + t.Body)) {
			if media.HasTemplateExpression(ref.Source) {
				continue
			}
			id, filename, protected, parseErr := media.ProtectedReference(ref.Source, root)
			if !protected {
				if ref.Image {
					external[ref.Source] = true
				}
				continue
			}
			m, exists := byID[id]
			if id == 0 && parseErr == nil {
				var matches []storedMedia
				for _, candidate := range inv.Media {
					if candidate.Filename == filename && candidate.Owner == c.Owner && sameOrg(candidate.Organization, c.Organization) {
						matches = append(matches, candidate)
					}
				}
				if len(matches) == 1 {
					m = matches[0]
					id = m.ID
					exists = true
				}
			}
			problem := ""
			switch {
			case parseErr != nil:
				problem = "invalid_reference"
			case !exists || filename != "" && filename != m.Filename:
				problem = "missing_or_stale_media"
			case m.Pending != nil:
				problem = "media_transfer_pending"
			case contains(c.Media, id) || contains(t.Media, id):
				continue
			case m.Owner == c.Owner && sameOrg(m.Organization, c.Organization) && m.Folder == nil:
				problem = "missing_association_repairable"
				org := "NULL"
				if c.Organization != nil {
					org = fmt.Sprint(*c.Organization)
				}
				encodedBody := base64.StdEncoding.EncodeToString([]byte(c.Body))
				encodedName := base64.StdEncoding.EncodeToString([]byte(m.Filename))
				fmt.Fprintf(&sql, "PERFORM 1 FROM campaigns c JOIN media m ON m.id=%d WHERE c.id=%d AND c.owner_user_id=%d AND m.owner_user_id=c.owner_user_id AND c.organization_id IS NOT DISTINCT FROM %s::BIGINT AND m.organization_id IS NOT DISTINCT FROM c.organization_id AND c.transfer_pending_at IS NULL AND m.transfer_pending_at IS NULL AND m.folder_id IS NULL AND m.filename=convert_from(decode('%s','base64'),'UTF8') AND c.body=convert_from(decode('%s','base64'),'UTF8') FOR UPDATE OF c,m;\nIF NOT FOUND THEN RAISE EXCEPTION 'campaign %d or media %d changed; audit again'; END IF;\nINSERT INTO campaign_media(campaign_id,media_id,filename) SELECT %d,id,filename FROM media WHERE id=%d ON CONFLICT(campaign_id,media_id) DO NOTHING;\n", id, c.ID, c.Owner, org, encodedName, encodedBody, c.ID, id, c.ID, id)
			default:
				problem = "missing_association_requires_owner_review"
			}
			findings = append(findings, finding{c.ID, c.Status, c.Sent, ref.Source, id, problem})
		}
	}
	sql.WriteString("END $repair$;\nSELECT id,status,sent FROM campaigns WHERE id IN (66,68,70,109,110) ORDER BY id;\nCOMMIT;\n")
	result := struct {
		Campaigns int             `json:"campaign_count"`
		Templates int             `json:"template_count"`
		Media     int             `json:"media_count"`
		Findings  []finding       `json:"findings"`
		External  map[string]bool `json:"external_image_urls_to_verify"`
	}{len(inv.Campaigns), len(inv.Templates), len(inv.Media), findings, external}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(output, encoded, 0600); err != nil {
		return err
	}
	if repair != "" {
		if err := os.WriteFile(repair, []byte(sql.String()), 0600); err != nil {
			return err
		}
	}
	fmt.Printf("Audited %d campaigns, %d templates, %d media: %d findings; %d external image URLs require anonymous verification\n", len(inv.Campaigns), len(inv.Templates), len(inv.Media), len(findings), len(external))
	return nil
}
