package migrations

import (
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V6_75_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf, _ *log.Logger) error {
	_, err := db.Exec(CampaignAttachmentLangSQL)
	return err
}

// CampaignAttachmentLangSQL adds the manual, campaign-wide language of the
// appended attachment section. Existing campaigns keep the default English;
// the earlier per-recipient "auto" mode is folded into English so the stored
// value always names a language the sender can render.
const CampaignAttachmentLangSQL = `
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS attachment_lang TEXT NOT NULL DEFAULT 'en';
ALTER TABLE campaigns ALTER COLUMN attachment_lang SET DEFAULT 'en';
UPDATE campaigns SET attachment_lang = 'en' WHERE attachment_lang = 'auto';
ALTER TABLE campaigns DROP CONSTRAINT IF EXISTS campaigns_attachment_lang_check;
ALTER TABLE campaigns ADD CONSTRAINT campaigns_attachment_lang_check
    CHECK (attachment_lang IN ('en','zh-CN','zh-TW','es','fr','de','pt','it','ru','ja','ko'));
`
