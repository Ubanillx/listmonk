package models

import (
	"time"

	null "gopkg.in/volatiletech/null.v6"
)

type CampaignReportSummary struct {
	CampaignID     int      `json:"campaign_id"`
	Sent           int      `json:"sent"`
	Bounced        int      `json:"bounced"`
	ViewsTotal     int      `json:"views_total"`
	ClicksTotal    int      `json:"clicks_total"`
	UniqueViewers  *int     `json:"unique_viewers"`
	UniqueClickers *int     `json:"unique_clickers"`
	OpenRate       *float64 `json:"open_rate"`
	ClickRate      *float64 `json:"click_rate"`
	CTOR           *float64 `json:"ctor"`
}

type CampaignReportSeries struct {
	Views   []CampaignAnalyticsCount `json:"views"`
	Clicks  []CampaignAnalyticsCount `json:"clicks"`
	Bounces []CampaignAnalyticsCount `json:"bounces"`
}

type CampaignReportLinkRow struct {
	LinkID          int      `db:"link_id" json:"link_id"`
	URL             string   `db:"url" json:"url"`
	TotalClicks     int      `db:"total_clicks" json:"total_clicks"`
	UniqueClickers  *int     `json:"unique_clickers"`
	UniqueClickRate *float64 `json:"unique_click_rate"`
}

type CampaignReportRecipientRow struct {
	SubscriberID    int         `db:"subscriber_id" json:"subscriber_id"`
	UUID            string      `db:"uuid" json:"uuid"`
	Email           string      `db:"email" json:"email"`
	Name            string      `db:"name" json:"name"`
	RecipientStatus string      `db:"recipient_status" json:"recipient_status"`
	SentAt          null.Time   `db:"sent_at" json:"sent_at"`
	BounceCount     int         `db:"bounce_count" json:"bounce_count"`
	ViewCount       int         `db:"view_count" json:"view_count"`
	ClickCount      int         `db:"click_count" json:"click_count"`
	FirstViewedAt   null.Time   `db:"first_viewed_at" json:"first_viewed_at"`
	LastViewedAt    null.Time   `db:"last_viewed_at" json:"last_viewed_at"`
	FirstClickedAt  null.Time   `db:"first_clicked_at" json:"first_clicked_at"`
	LastClickedAt   null.Time   `db:"last_clicked_at" json:"last_clicked_at"`
	LastLinkID      null.Int    `db:"last_link_id" json:"last_link_id"`
	LastLinkURL     null.String `db:"last_link_url" json:"last_link_url"`
	LastEngagedAt   null.Time   `db:"last_engaged_at" json:"last_engaged_at"`
	Total           int         `db:"total" json:"-"`
}

type CampaignReportRecipientFilters struct {
	Search  string
	Opened  string
	Clicked string
	Bounced string
	LinkID  int
	SortBy  string
	Order   string
}

type CampaignReportSummaryDB struct {
	CampaignID     int `db:"campaign_id"`
	Sent           int `db:"sent"`
	Bounced        int `db:"bounced"`
	ViewsTotal     int `db:"views_total"`
	ClicksTotal    int `db:"clicks_total"`
	UniqueViewers  int `db:"unique_viewers"`
	UniqueClickers int `db:"unique_clickers"`
}

type CampaignsReportSummaryDB struct {
	Sent           int `db:"sent"`
	Bounced        int `db:"bounced"`
	ViewsTotal     int `db:"views_total"`
	ClicksTotal    int `db:"clicks_total"`
	UniqueViewers  int `db:"unique_viewers"`
	UniqueClickers int `db:"unique_clickers"`
}

type CampaignReportLinkRowDB struct {
	LinkID         int    `db:"link_id"`
	URL            string `db:"url"`
	TotalClicks    int    `db:"total_clicks"`
	UniqueClickers int    `db:"unique_clickers"`
}

type CampaignsReportLinkRow struct {
	CampaignID      int      `db:"campaign_id" json:"campaign_id"`
	CampaignName    string   `db:"campaign_name" json:"campaign_name"`
	CampaignSubject string   `db:"campaign_subject" json:"campaign_subject"`
	LinkID          int      `db:"link_id" json:"link_id"`
	URL             string   `db:"url" json:"url"`
	TotalClicks     int      `db:"total_clicks" json:"total_clicks"`
	UniqueClickers  *int     `json:"unique_clickers"`
	UniqueClickRate *float64 `json:"unique_click_rate"`
}

type CampaignsReportLinkRowDB struct {
	CampaignID      int    `db:"campaign_id"`
	CampaignName    string `db:"campaign_name"`
	CampaignSubject string `db:"campaign_subject"`
	LinkID          int    `db:"link_id"`
	URL             string `db:"url"`
	TotalClicks     int    `db:"total_clicks"`
	UniqueClickers  int    `db:"unique_clickers"`
	Sent            int    `db:"sent"`
}

type CampaignsReportRecipientRow struct {
	CampaignID      int         `db:"campaign_id" json:"campaign_id"`
	CampaignName    string      `db:"campaign_name" json:"campaign_name"`
	CampaignSubject string      `db:"campaign_subject" json:"campaign_subject"`
	SubscriberID    int         `db:"subscriber_id" json:"subscriber_id"`
	UUID            string      `db:"uuid" json:"uuid"`
	Email           string      `db:"email" json:"email"`
	Name            string      `db:"name" json:"name"`
	RecipientStatus string      `db:"recipient_status" json:"recipient_status"`
	SentAt          null.Time   `db:"sent_at" json:"sent_at"`
	BounceCount     int         `db:"bounce_count" json:"bounce_count"`
	ViewCount       int         `db:"view_count" json:"view_count"`
	ClickCount      int         `db:"click_count" json:"click_count"`
	FirstViewedAt   null.Time   `db:"first_viewed_at" json:"first_viewed_at"`
	LastViewedAt    null.Time   `db:"last_viewed_at" json:"last_viewed_at"`
	FirstClickedAt  null.Time   `db:"first_clicked_at" json:"first_clicked_at"`
	LastClickedAt   null.Time   `db:"last_clicked_at" json:"last_clicked_at"`
	LastLinkID      null.Int    `db:"last_link_id" json:"last_link_id"`
	LastLinkURL     null.String `db:"last_link_url" json:"last_link_url"`
	LastEngagedAt   null.Time   `db:"last_engaged_at" json:"last_engaged_at"`
	Total           int         `db:"total" json:"-"`
}

type CampaignReportRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}
