package models

type CampaignDeliveryOrganization struct {
	OrganizationID int    `db:"organization_id" json:"organization_id"`
	Name           string `db:"name" json:"name"`
	Planned        int    `db:"planned" json:"planned"`
	Sent           int    `db:"sent" json:"sent"`
	Unsent         int    `db:"unsent" json:"unsent"`
	Failed         int    `db:"failed" json:"failed"`
}

type CampaignDeliveryOverview struct {
	CampaignID    int                            `db:"campaign_id" json:"campaign_id"`
	Status        string                         `db:"status" json:"status"`
	Source        string                         `json:"source"`
	Planned       int                            `db:"planned" json:"planned"`
	Sent          int                            `db:"sent" json:"sent"`
	Unsent        int                            `db:"unsent" json:"unsent"`
	Failed        int                            `db:"failed" json:"failed"`
	Waiting       int                            `json:"waiting"`
	Progress      float64                        `json:"progress"`
	SendErrors    int                            `db:"send_errors" json:"send_errors"`
	Organizations []CampaignDeliveryOrganization `json:"organizations"`
}
