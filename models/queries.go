package models

import (
	"context"
	"database/sql"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// Queries contains all prepared SQL queries.
type Queries struct {
	GetDashboardCharts *sqlx.Stmt `query:"get-dashboard-charts"`
	GetDashboardCounts *sqlx.Stmt `query:"get-dashboard-counts"`

	InsertCustomer                   *sqlx.Stmt `query:"insert-customer"`
	UpsertCustomer                   *sqlx.Stmt `query:"upsert-customer"`
	UpsertBlocklistCustomer          *sqlx.Stmt `query:"upsert-blocklist-customer"`
	UpsertWorkspaceCustomer          *sqlx.Stmt `query:"upsert-workspace-customer"`
	UpsertWorkspaceBlocklistCustomer *sqlx.Stmt `query:"upsert-workspace-blocklist-customer"`
	GetCustomer                      *sqlx.Stmt `query:"get-customer"`
	HasCustomerListMemberships       *sqlx.Stmt `query:"has-customer-list-memberships"`
	GetCustomersByEmails             *sqlx.Stmt `query:"get-customers-by-emails"`
	GetCustomerListMemberships       *sqlx.Stmt `query:"get-customer-list-memberships"`
	GetSubscriptions                 *sqlx.Stmt `query:"get-subscriptions"`
	GetCustomerListMembershipsLazy   *sqlx.Stmt `query:"get-customer-list-memberships-lazy"`
	UpdateCustomer                   *sqlx.Stmt `query:"update-customer"`
	UpdateCustomerWithLists          *sqlx.Stmt `query:"update-customer-with-customer-lists"`
	BlocklistCustomers               *sqlx.Stmt `query:"blocklist-customers"`
	AddCustomersToLists              *sqlx.Stmt `query:"add-customers-to-customer-lists"`
	DeleteSubscriptions              *sqlx.Stmt `query:"delete-subscriptions"`
	DeleteUnconfirmedSubscriptions   *sqlx.Stmt `query:"delete-unconfirmed-subscriptions"`
	ConfirmSubscriptionOptin         *sqlx.Stmt `query:"confirm-subscription-optin"`
	UnsubscribeCustomersFromLists    *sqlx.Stmt `query:"unsubscribe-customers-from-customer-lists"`
	DeleteCustomers                  *sqlx.Stmt `query:"delete-customers"`
	DeleteBlocklistedCustomers       *sqlx.Stmt `query:"delete-blocklisted-customers"`
	DeleteOrphanCustomers            *sqlx.Stmt `query:"delete-orphan-customers"`
	UnsubscribeByCampaign            *sqlx.Stmt `query:"unsubscribe-by-campaign"`
	ExportCustomerData               *sqlx.Stmt `query:"export-customer-data"`
	GetCustomerActivity              *sqlx.Stmt `query:"get-customer-activity"`

	// Non-prepared arbitrary customer queries.
	QueryCustomers                       string     `query:"query-customers"`
	QueryCustomersCount                  string     `query:"query-customers-count"`
	QueryCustomersCountAll               *sqlx.Stmt `query:"query-customers-count-all"`
	QueryCustomersForExport              string     `query:"query-customers-for-export"`
	QueryCustomersTpl                    string     `query:"query-customers-template"`
	DeleteCustomersByQuery               string     `query:"delete-customers-by-query"`
	AddCustomersToListsByQuery           string     `query:"add-customers-to-customer-lists-by-query"`
	BlocklistCustomersByQuery            string     `query:"blocklist-customers-by-query"`
	DeleteSubscriptionsByQuery           string     `query:"delete-subscriptions-by-query"`
	UnsubscribeCustomersFromListsByQuery string     `query:"unsubscribe-customers-from-customer-lists-by-query"`

	CreateList      *sqlx.Stmt `query:"create-customer-list"`
	QueryLists      string     `query:"query-customer-lists"`
	GetLists        *sqlx.Stmt `query:"get-customer-lists"`
	GetListsByOptin *sqlx.Stmt `query:"get-customer-lists-by-optin"`
	GetListTypes    *sqlx.Stmt `query:"get-customer-list-types"`
	UpdateList      *sqlx.Stmt `query:"update-customer-list"`
	UpdateListsDate *sqlx.Stmt `query:"update-customer-lists-date"`
	DeleteLists     *sqlx.Stmt `query:"delete-customer-lists"`

	CreateCampaign                 *sqlx.Stmt `query:"create-campaign"`
	QueryCampaigns                 string     `query:"query-campaigns"`
	GetCampaign                    *sqlx.Stmt `query:"get-campaign"`
	GetPublicCampaignRecipient     *sqlx.Stmt `query:"get-public-campaign-recipient"`
	GetPublicPoolCampaignRecipient *sqlx.Stmt `query:"get-public-pool-campaign-recipient"`
	GetCampaignForPreview          *sqlx.Stmt `query:"get-campaign-for-preview"`
	GetCampaignStats               *sqlx.Stmt `query:"get-campaign-stats"`
	GetCampaignStatus              *sqlx.Stmt `query:"get-campaign-status"`
	GetArchivedCampaigns           *sqlx.Stmt `query:"get-archived-campaigns"`
	CampaignHasLists               *sqlx.Stmt `query:"campaign-has-customer-lists"`
	GetCampaignCustomerListIDs     *sqlx.Stmt `query:"get-campaign-customer-list-ids"`

	// These two queries are read as strings and based on settings.individual_tracking=on/off,
	// are interpolated and copied to view and click counts. Same query, different tables.
	GetCampaignAnalyticsCounts     string     `query:"get-campaign-analytics-counts"`
	GetCampaignViewCounts          *sqlx.Stmt `query:"get-campaign-view-counts"`
	GetCampaignClickCounts         *sqlx.Stmt `query:"get-campaign-click-counts"`
	GetCampaignLinkCounts          *sqlx.Stmt `query:"get-campaign-link-counts"`
	GetCampaignBounceCounts        *sqlx.Stmt `query:"get-campaign-bounce-counts"`
	GetCampaignReportSummary       *sqlx.Stmt `query:"get-campaign-report-summary"`
	GetCampaignsReportSummary      *sqlx.Stmt `query:"get-campaigns-report-summary"`
	GetCampaignReportLinks         *sqlx.Stmt `query:"get-campaign-report-links"`
	GetCampaignsReportLinks        *sqlx.Stmt `query:"get-campaigns-report-links"`
	QueryCampaignReportRecipients  string     `query:"query-campaign-report-recipients"`
	QueryCampaignsReportRecipients string     `query:"query-campaigns-report-recipients"`
	DeleteCampaignViews            *sqlx.Stmt `query:"delete-campaign-views"`
	DeleteCampaignLinkClicks       *sqlx.Stmt `query:"delete-campaign-link-clicks"`

	NextCampaigns                   *sqlx.Stmt `query:"next-campaigns"`
	GetCampaignSendState            *sqlx.Stmt `query:"get-campaign-send-state"`
	HasCampaignRecipients           *sqlx.Stmt `query:"has-campaign-recipients"`
	EnsureCampaignRecipients        *sqlx.Stmt `query:"ensure-campaign-recipients"`
	SyncCampaignProgress            *sqlx.Stmt `query:"sync-campaign-progress"`
	SnapshotCampaignRecipients      *sqlx.Stmt `query:"snapshot-campaign-recipients"`
	SetCampaignRunning              *sqlx.Stmt `query:"set-campaign-running"`
	SetCampaignDeferred             *sqlx.Stmt `query:"set-campaign-deferred"`
	NextCampaignCustomers           *sqlx.Stmt `query:"queue-campaign-customers"`
	NextCampaignPoolCustomers       *sqlx.Stmt `query:"queue-campaign-pool-customers"`
	MarkCampaignRecipientSent       *sqlx.Stmt `query:"mark-campaign-recipient-sent"`
	MarkCampaignRecipientStatus     *sqlx.Stmt `query:"mark-campaign-recipient-status"`
	ResetCampaignQueuedRecipients   *sqlx.Stmt `query:"reset-campaign-queued-recipients"`
	UpdateCampaignRecipientStatuses *sqlx.Stmt `query:"update-campaign-recipient-statuses"`
	IncrementCampaignDailyUsage     *sqlx.Stmt `query:"increment-campaign-daily-usage"`
	GetOneCampaignCustomer          *sqlx.Stmt `query:"get-one-campaign-customer"`
	UpdateCampaign                  *sqlx.Stmt `query:"update-campaign"`
	UpdateCampaignStatus            *sqlx.Stmt `query:"update-campaign-status"`
	UpdateCampaignCounts            *sqlx.Stmt `query:"update-campaign-counts"`
	UpdateCampaignArchive           *sqlx.Stmt `query:"update-campaign-archive"`
	RegisterCampaignView            *sqlx.Stmt `query:"register-campaign-view"`
	DeleteCampaign                  *sqlx.Stmt `query:"delete-campaign"`
	DeleteCampaigns                 *sqlx.Stmt `query:"delete-campaigns"`

	InsertMedia *sqlx.Stmt `query:"insert-media"`
	GetMedia    *sqlx.Stmt `query:"get-media"`
	QueryMedia  *sqlx.Stmt `query:"query-media"`
	DeleteMedia *sqlx.Stmt `query:"delete-media"`

	CreateTemplate     *sqlx.Stmt `query:"create-template"`
	GetTemplates       *sqlx.Stmt `query:"get-templates"`
	UpdateTemplate     *sqlx.Stmt `query:"update-template"`
	SetDefaultTemplate *sqlx.Stmt `query:"set-default-template"`
	DeleteTemplate     *sqlx.Stmt `query:"delete-template"`

	CreateLink        *sqlx.Stmt `query:"create-link"`
	GetLinkURL        *sqlx.Stmt `query:"get-link-url"`
	RegisterLinkClick *sqlx.Stmt `query:"register-link-click"`

	GetSettings                 *sqlx.Stmt `query:"get-settings"`
	UpdateSettings              *sqlx.Stmt `query:"update-settings"`
	UpdateSettingsByKey         *sqlx.Stmt `query:"update-settings-by-key"`
	GetSMTPDailyUsage           *sqlx.Stmt `query:"get-smtp-daily-usage"`
	IncrementSMTPDailyUsage     *sqlx.Stmt `query:"increment-smtp-daily-usage"`
	GetUserSMTPServers          *sqlx.Stmt `query:"get-user-smtp-servers"`
	GetEnabledUserSMTPServers   *sqlx.Stmt `query:"get-enabled-user-smtp-servers"`
	GetUserSMTPServer           *sqlx.Stmt `query:"get-user-smtp-server"`
	CreateUserSMTPServer        *sqlx.Stmt `query:"create-user-smtp-server"`
	UpdateUserSMTPServer        *sqlx.Stmt `query:"update-user-smtp-server"`
	DeleteUserSMTPServer        *sqlx.Stmt `query:"delete-user-smtp-server"`
	HasUserRunningCampaigns     *sqlx.Stmt `query:"has-user-running-campaigns"`
	GetUserSMTPDailyUsage       *sqlx.Stmt `query:"get-user-smtp-daily-usage"`
	IncrementUserSMTPDailyUsage *sqlx.Stmt `query:"increment-user-smtp-daily-usage"`
	GetUserSMTPRemaining        *sqlx.Stmt `query:"get-user-smtp-remaining"`

	GetReplyMailboxes        *sqlx.Stmt `query:"get-reply-mailboxes"`
	GetReplyMailbox          *sqlx.Stmt `query:"get-reply-mailbox"`
	CreateReplyMailbox       *sqlx.Stmt `query:"create-reply-mailbox"`
	UpdateReplyMailbox       *sqlx.Stmt `query:"update-reply-mailbox"`
	DisableReplyMailbox      *sqlx.Stmt `query:"disable-reply-mailbox"`
	GetReplyAIMailboxes      *sqlx.Stmt `query:"get-reply-ai-mailboxes"`
	GetReplyAIMailbox        *sqlx.Stmt `query:"get-reply-ai-mailbox"`
	InsertReplyAIEvent       *sqlx.Stmt `query:"insert-reply-ai-event"`
	ClaimReplyAIEvent        *sqlx.Stmt `query:"claim-reply-ai-event"`
	FinishReplyAIEvent       *sqlx.Stmt `query:"finish-reply-ai-event"`
	FailReplyAIEvent         *sqlx.Stmt `query:"fail-reply-ai-event"`
	UpdateReplyAIMailboxSync *sqlx.Stmt `query:"update-reply-ai-mailbox-sync"`

	// GetStats *sqlx.Stmt `query:"get-stats"`
	RecordBounce              *sqlx.Stmt `query:"record-bounce"`
	QueryBounces              string     `query:"query-bounces"`
	BlocklistBouncedCustomers *sqlx.Stmt `query:"blocklist-bounced-customers"`
	DeleteBounces             *sqlx.Stmt `query:"delete-bounces"`
	DeleteBouncesByCustomer   *sqlx.Stmt `query:"delete-bounces-by-customer"`
	GetDBInfo                 string     `query:"get-db-info"`

	CreateUser                           *sqlx.Stmt `query:"create-user"`
	UpdateUser                           *sqlx.Stmt `query:"update-user"`
	UpdateUserProfile                    *sqlx.Stmt `query:"update-user-profile"`
	UpdateUserLogin                      *sqlx.Stmt `query:"update-user-login"`
	SetUserTwoFA                         *sqlx.Stmt `query:"set-user-twofa"`
	DeleteUsers                          *sqlx.Stmt `query:"delete-users"`
	GetUsers                             *sqlx.Stmt `query:"get-users"`
	GetUser                              *sqlx.Stmt `query:"get-user"`
	GetAPITokens                         *sqlx.Stmt `query:"get-api-tokens"`
	CreateIntegrationToken               *sqlx.Stmt `query:"create-integration-token"`
	GetIntegrationTokens                 *sqlx.Stmt `query:"get-integration-tokens"`
	GetPersonalIntegrationTokens         *sqlx.Stmt `query:"get-personal-integration-tokens"`
	GetActiveIntegrationTokens           *sqlx.Stmt `query:"get-active-integration-tokens"`
	CreatePersonalIntegrationToken       *sqlx.Stmt `query:"create-personal-integration-token"`
	CountActivePersonalIntegrationTokens *sqlx.Stmt `query:"count-active-personal-integration-tokens"`
	UpdatePersonalIntegrationToken       *sqlx.Stmt `query:"update-personal-integration-token"`
	DeleteIntegrationToken               *sqlx.Stmt `query:"delete-integration-token"`
	DeletePersonalIntegrationToken       *sqlx.Stmt `query:"delete-personal-integration-token"`
	UpdateIntegrationTokenUsage          *sqlx.Stmt `query:"update-integration-token-usage"`
	LoginUser                            *sqlx.Stmt `query:"login-user"`

	CreateRole            *sqlx.Stmt `query:"create-role"`
	GetUserRoles          *sqlx.Stmt `query:"get-user-roles"`
	GetListRoles          *sqlx.Stmt `query:"get-customer_list-roles"`
	UpdateRole            *sqlx.Stmt `query:"update-role"`
	DeleteRole            *sqlx.Stmt `query:"delete-role"`
	UpsertListPermissions *sqlx.Stmt `query:"upsert-customer_list-permissions"`
	DeleteListPermission  *sqlx.Stmt `query:"delete-customer_list-permission"`
}

// compileCustomerQueryTpl takes an arbitrary WHERE expressions
// to filter customers from the customers table and prepares a query
// out of it using the raw `query-customers-template` query template.
// While doing this, a readonly transaction is created and the query is
// dry run on it to ensure that it is indeed readonly.
func (q *Queries) compileCustomerQueryTpl(searchStr, queryExp string, db *sqlx.DB, subStatus string) (string, error) {
	tx, err := db.BeginTxx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// There's an arbitrary query condition.
	cond := "TRUE"
	if queryExp != "" {
		cond = queryExp
	}

	// Perform the dry run.
	stmt := strings.ReplaceAll(q.QueryCustomersTpl, "%query%", cond)
	if _, err := tx.Exec(stmt, true, pq.Int64Array{}, subStatus, searchStr); err != nil {
		return "", err
	}

	return stmt, nil
}

// compileCustomerQueryTpl takes an arbitrary WHERE expressions and a customer
// query template that depends on the filter (eg: delete by query, blocklist by query etc.)
// combines and executes them.
func (q *Queries) ExecSubQueryTpl(searchStr, queryExp, baseQueryTpl string, customerListIDs []int, db *sqlx.DB, subStatus string, args ...any) error {
	// Perform a dry run.
	filterExp, err := q.compileCustomerQueryTpl(searchStr, queryExp, db, subStatus)
	if err != nil {
		return err
	}

	if len(customerListIDs) == 0 {
		customerListIDs = []int{}
	}

	// Insert the customer filter query into the target query.
	stmt := strings.ReplaceAll(baseQueryTpl, "%query%", filterExp)

	// First argument is the boolean indicating if the query is a dry run.
	a := append([]any{false, pq.Array(customerListIDs), subStatus, searchStr}, args...)

	// Execute the query on the DB.
	if _, err := db.Exec(stmt, a...); err != nil {
		return err
	}
	return nil
}
