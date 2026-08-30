package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// CloneCampaignForWorkspace keeps the historical API shape for callers that
// already selected the source and destination in the same workspace. HTTP
// handlers and migrations use CloneCampaignForWorkspaceWithSource so the
// source workspace is revalidated independently while the clone transaction is
// running.
func (c *Core) CloneCampaignForWorkspace(sourceID int, target models.WorkspaceAccess, name string) (models.Campaign, error) {
	return c.CloneCampaignForWorkspaceWithSource(sourceID, target, target, name)
}

// CloneCampaignForWorkspaceWithSource creates an independent draft in target.
// Both the source and destination workspace are checked again under database
// locks. This closes the interval in which a member could leave, an
// organization could be archived, or a source media row could be deleted after
// the HTTP authorization check but before the snapshot is written.
func (c *Core) CloneCampaignForWorkspaceWithSource(sourceID int, sourceAccess, target models.WorkspaceAccess, name string) (models.Campaign, error) {
	tx, lockedOrganizations, err := c.beginCloneTransaction("campaigns", sourceID, sourceAccess, target)
	if err != nil {
		return models.Campaign{}, err
	}
	defer tx.Rollback()

	sourceScope, err := c.lockCloneResourceScope(tx, resourceCampaigns, sourceID, lockedOrganizations)
	if err != nil {
		return models.Campaign{}, err
	}
	if !c.CanCopyCampaign(sourceAccess, sourceScope) {
		return models.Campaign{}, echo.NewHTTPError(http.StatusForbidden, "campaign is not copyable in the active workspace")
	}

	// The campaign row is locked before its associations are read. Normal
	// campaign updates use the same lock, so the list/media/template snapshot is
	// consistent with the campaign fields below.
	source, err := c.getCampaignCloneSourceTx(tx, sourceID)
	if err != nil {
		return models.Campaign{}, err
	}
	var sourceTemplate *models.Template
	if source.TemplateID.Valid && source.TemplateID.Int > 0 {
		tplScope, err := c.lockCloneResourceScope(tx, resourceTemplates, int(source.TemplateID.Int), lockedOrganizations)
		if err != nil {
			return models.Campaign{}, err
		}
		if tplScope.TransferPendingAt.Valid || tplScope.OrganizationArchived {
			return models.Campaign{}, workspaceMutationError()
		}
		tpl, err := c.getTemplateCloneSourceTx(tx, int(source.TemplateID.Int))
		if err != nil {
			return models.Campaign{}, err
		}
		if err := c.validateCampaignCloneTemplate(tx, sourceID, int(source.TemplateID.Int)); err != nil {
			return models.Campaign{}, err
		}
		sourceTemplate = &tpl
	}

	associations, err := c.lockCampaignCloneMedia(tx, sourceID, source.TemplateID)
	if err != nil {
		return models.Campaign{}, err
	}
	if err := c.ensureCloneRelatedOrganizations(tx, "campaigns", sourceID, lockedOrganizations); err != nil {
		return models.Campaign{}, err
	}

	if strings.TrimSpace(name) != "" {
		source.Name = strings.TrimSpace(name)
	}
	newUUID, err := uuid.NewV4()
	if err != nil {
		return models.Campaign{}, echo.NewHTTPError(http.StatusInternalServerError,
			c.i18n.Ts("globals.messages.errorUUID", "error", err.Error()))
	}
	targetScope := ApplyWorkspaceScope(target, models.ResourceVisibilityPrivate)
	headers := cloneCampaignHeaders(source.Headers)
	mediaCopies, err := c.cloneMediaAssociations(tx, associations, targetScope)
	if err != nil {
		return models.Campaign{}, err
	}
	// The canonical media URL contains the source row ID. Rewrite both the
	// campaign body and its optional visual-editor source to point at the
	// independent copies created above. A cloned campaign also receives a
	// private template snapshot below, so rewrite that template's body before
	// inserting it; otherwise its HTML would continue pointing at the source
	// media IDs and could break as soon as the source resource is removed.
	sourceNames := mediaAssociationSourceNames(associations, mediaCopies)
	source.Body = string(rewriteMediaReferences([]byte(source.Body), mediaCopies, sourceNames))
	if source.BodySource.Valid {
		source.BodySource.String = string(rewriteMediaReferences([]byte(source.BodySource.String), mediaCopies, sourceNames))
	}
	if sourceTemplate != nil {
		sourceTemplate.Body = string(rewriteMediaReferences([]byte(sourceTemplate.Body), mediaCopies, sourceNames))
		if sourceTemplate.BodySource.Valid {
			sourceTemplate.BodySource.String = string(rewriteMediaReferences([]byte(sourceTemplate.BodySource.String), mediaCopies, sourceNames))
		}
	}
	if source.AltBody.Valid {
		source.AltBody.String = string(rewriteMediaReferences([]byte(source.AltBody.String), mediaCopies, sourceNames))
	}

	// A campaign clone always gets an independent template snapshot. This also
	// makes a private/shared source template and all inline media independent of
	// later edits or deletion of the source.
	newTemplateID := null.Int{}
	if sourceTemplate != nil {
		var templateID int
		if err := tx.Get(&templateID, `
			INSERT INTO templates (
				name, type, subject, body, body_source, is_default,
				organization_id, owner_user_id, original_owner_user_id, visibility
			) VALUES ($1, $2, $3, $4, $5, FALSE, $6, $7, $8, 'private')
			RETURNING id`, sourceTemplate.Name+" (copy)", sourceTemplate.Type,
			sourceTemplate.Subject, sourceTemplate.Body, sourceTemplate.BodySource,
			targetScope.OrganizationID, targetScope.OwnerUserID, targetScope.OriginalOwnerUserID); err != nil {
			return models.Campaign{}, workspaceQueryError("copying campaign template", err)
		}
		for _, association := range associations.Template {
			if err := insertMediaAssociation(tx, "template_media", "template_id", templateID, association, mediaCopies); err != nil {
				return models.Campaign{}, err
			}
		}
		newTemplateID = null.Int{Int: templateID, Valid: true}
	}

	// Keep the strict campaign default even when cloning a legacy row that was
	// stored with daily_send_limit=0 before the 300-message default existed.
	dailySendLimit := source.DailySendLimit
	messenger := source.Messenger
	if email.IsMessengerName(messenger) {
		messenger = "email"
		if source.Type == models.CampaignTypeRegular && dailySendLimit < 1 {
			dailySendLimit = 300
		}
	}

	var newID int
	if err := tx.Get(&newID, `
		INSERT INTO campaigns (
			uuid, type, name, subject, from_email, body, body_source, altbody,
			content_type, send_at, headers, attribs, status, daily_send_limit,
			daily_resume_time, tags, messenger, template_id, to_send, sent,
			max_subscriber_id, last_subscriber_id, archive, archive_slug,
			archive_template_id, archive_meta, auto_track_links,
			organization_id, owner_user_id, original_owner_user_id, visibility
		) VALUES (
			$1, $2, $3, $4, '', $5, $6, $7,
			$8, NULL, $9, $10, 'draft', $11,
			$12, $13, $14, $15, 0, 0,
			0, 0, FALSE, NULL,
			NULL, '{}'::JSONB, $16,
			$17, $18, $19, 'private'
		) RETURNING id`,
		newUUID, source.Type, source.Name, source.Subject, source.Body,
		source.BodySource, source.AltBody, source.ContentType, headers,
		source.Attribs, dailySendLimit, source.DailyResumeTime,
		pq.StringArray(normalizeTags(source.Tags)), messenger, newTemplateID,
		source.AutoTrackLinks, targetScope.OrganizationID, targetScope.OwnerUserID,
		targetScope.OriginalOwnerUserID); err != nil {
		return models.Campaign{}, workspaceQueryError("creating campaign clone", err)
	}

	// Campaign media records carry the original filenames used by CID/MIME
	// attachment assembly. Sending lists and recipient/history rows are
	// deliberately not copied.
	for _, association := range associations.Campaign {
		if err := insertMediaAssociation(tx, "campaign_media", "campaign_id", newID, association, mediaCopies); err != nil {
			return models.Campaign{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.Campaign{}, workspaceQueryError("committing campaign clone", err)
	}
	// Resolve the response through the destination workspace predicate as well.
	// The clone transaction has already committed, so a legacy unscoped read
	// here would re-open a race in which the destination membership or
	// organization state changes between commit and response serialization.
	return c.GetWorkspaceCampaign(target, newID)
}

type mediaAssociation struct {
	MediaID  null.Int `db:"media_id"`
	Filename string   `db:"filename"`
}

// cloneMediaAssociations separates campaign and template relationships while
// allowing one source media row to be copied only once per clone transaction.
// Keeping the association filename is important for CID/MIME rendering.
type cloneMediaAssociations struct {
	Campaign []mediaAssociation
	Template []mediaAssociation
}

// visualCampaignMediaSnapshot is the transaction-local result of importing a
// visual campaign template. Visual templates are copied into a campaign's
// body (the campaign does not retain template_id), so media referenced by the
// template must be copied into the destination workspace as well. Keeping the
// copied IDs and rewritten bodies together prevents a request from committing
// a body that still points at another user's binary.
type visualCampaignMediaSnapshot struct {
	MediaIDs   []int
	Body       string
	BodySource null.String
	AltBody    null.String
}

// snapshotVisualCampaignMedia validates a visual template and snapshots the
// selected template media into the destination campaign workspace. A shared
// template may contain private media owned by its author; copying those rows
// here is the safe way to make an imported visual campaign independent of the
// source template and its later deletion. The template ID and every media
// association are revalidated while locked, so a client cannot grant itself a
// private media row by merely submitting its numeric ID.
func (c *Core) snapshotVisualCampaignMedia(tx *sqlx.Tx, access models.WorkspaceAccess, target models.ResourceScope, templateID int, mediaIDs []int, body string, bodySource, altBody null.String) (visualCampaignMediaSnapshot, error) {
	out := visualCampaignMediaSnapshot{
		MediaIDs:   uniqueMutationIDs(mediaIDs),
		Body:       body,
		BodySource: bodySource,
		AltBody:    altBody,
	}
	if templateID < 1 {
		return out, workspaceMutationError()
	}
	if !target.OwnerUserID.Valid || target.OwnerUserID.Int < 1 {
		return out, workspaceMutationError()
	}

	var source struct {
		Type              string      `db:"type"`
		OrganizationID    null.Int    `db:"organization_id"`
		OwnerUserID       null.Int    `db:"owner_user_id"`
		OriginalOwnerID   null.Int    `db:"original_owner_user_id"`
		Visibility        string      `db:"visibility"`
		TransferPendingAt null.Time   `db:"transfer_pending_at"`
		OrganizationState string      `db:"organization_state"`
		Body              string      `db:"body"`
		BodySource        null.String `db:"body_source"`
	}
	if err := tx.Get(&source, `
		SELECT t.type, t.organization_id, t.owner_user_id,
			t.original_owner_user_id, t.visibility, t.transfer_pending_at,
			t.body, t.body_source,
			CASE WHEN t.organization_id IS NULL THEN 'active'
				 ELSE COALESCE(o.status, 'archived') END AS organization_state
		FROM templates t
		LEFT JOIN organizations o ON o.id = t.organization_id
		WHERE t.id = $1
		FOR UPDATE OF t`, templateID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, workspaceQueryError("locking visual template", err)
	}

	sourceScope := models.ResourceScope{
		OrganizationID:       source.OrganizationID,
		OwnerUserID:          source.OwnerUserID,
		OriginalOwnerUserID:  source.OriginalOwnerID,
		Visibility:           source.Visibility,
		TransferPendingAt:    source.TransferPendingAt,
		OrganizationArchived: source.OrganizationState != "active",
	}
	if source.Type != models.TemplateTypeCampaignVisual || !c.CanUseResource(access, sourceScope) {
		return out, echo.NewHTTPError(http.StatusForbidden, "visual template cannot be used in the active workspace")
	}

	// lockTemplateCloneMedia locks every association and validates that each
	// binary belongs to the template's workspace/owner graph. This is stricter
	// than a client-provided media list and closes concurrent delete/transfer
	// races before any destination row is inserted.
	refs, err := c.lockTemplateCloneMedia(tx, templateID)
	if err != nil {
		return out, err
	}
	refByID := make(map[int]mediaAssociation, len(refs))
	for _, ref := range refs {
		if ref.MediaID.Valid {
			refByID[int(ref.MediaID.Int)] = ref
		}
	}
	// The media array submitted by an editor is a convenience hint, not the
	// authoritative template graph.  Older clients (and visual-editor imports
	// made before the media panel was opened) may omit one or more
	// template_media rows even though the locked template body still references
	// them.  Include every associated binary in the snapshot; otherwise the
	// cloned body would retain a source URL that is not backed by an independent
	// destination row.  Explicit client IDs remain in front so direct campaign
	// media keep their submitted ordering.
	requested := make(map[int]struct{}, len(out.MediaIDs))
	for _, id := range out.MediaIDs {
		if id > 0 {
			requested[id] = struct{}{}
		}
	}
	for _, ref := range refs {
		if !ref.MediaID.Valid {
			continue
		}
		id := int(ref.MediaID.Int)
		if id > 0 {
			if _, exists := requested[id]; !exists {
				out.MediaIDs = append(out.MediaIDs, id)
				requested[id] = struct{}{}
			}
		}
	}

	// An empty body can occur when an API client imports a template without
	// first materializing it in the editor. Use the locked source body in that
	// case; otherwise preserve the explicitly submitted body exactly.
	if strings.TrimSpace(out.Body) == "" {
		out.Body = source.Body
	}
	if !out.BodySource.Valid && source.BodySource.Valid {
		out.BodySource = source.BodySource
	}

	copies := make(map[int]int)
	sourceNames := make(map[int]string)
	for i, id := range out.MediaIDs {
		ref, ok := refByID[id]
		if !ok {
			continue
		}
		copyID, err := cloneMediaRecord(tx, id, target)
		if err != nil {
			return out, err
		}
		out.MediaIDs[i] = copyID
		copies[id] = copyID
		sourceNames[id] = ref.Filename
	}
	if len(copies) > 0 {
		out.Body = string(rewriteMediaReferences([]byte(out.Body), copies, sourceNames))
		if out.BodySource.Valid {
			out.BodySource.String = string(rewriteMediaReferences([]byte(out.BodySource.String), copies, sourceNames))
		}
		if out.AltBody.Valid {
			out.AltBody.String = string(rewriteMediaReferences([]byte(out.AltBody.String), copies, sourceNames))
		}
	}

	return out, nil
}

// CloneTemplateForWorkspace keeps the historical API shape for callers that
// selected source and destination in one workspace.
func (c *Core) CloneTemplateForWorkspace(sourceID int, target models.WorkspaceAccess, name, subject string) (models.Template, error) {
	return c.CloneTemplateForWorkspaceWithSource(sourceID, target, target, name, subject)
}

// CloneTemplateForWorkspaceWithSource copies a readable template into the
// caller's selected workspace. Source and destination membership, organization
// state, the template row, and every referenced media row are all checked while
// their locks are held.
func (c *Core) CloneTemplateForWorkspaceWithSource(sourceID int, sourceAccess, target models.WorkspaceAccess, name, subject string) (models.Template, error) {
	tx, lockedOrganizations, err := c.beginCloneTransaction("templates", sourceID, sourceAccess, target)
	if err != nil {
		return models.Template{}, err
	}
	defer tx.Rollback()

	scope, err := c.lockCloneResourceScope(tx, resourceTemplates, sourceID, lockedOrganizations)
	if err != nil {
		return models.Template{}, err
	}
	if !c.CanCopyResource(sourceAccess, scope) {
		return models.Template{}, echo.NewHTTPError(http.StatusForbidden, "template is not copyable in the active workspace")
	}
	source, err := c.getTemplateCloneSourceTx(tx, sourceID)
	if err != nil {
		return models.Template{}, err
	}
	associations, err := c.lockTemplateCloneMedia(tx, sourceID)
	if err != nil {
		return models.Template{}, err
	}
	if err := c.ensureCloneRelatedOrganizations(tx, "templates", sourceID, lockedOrganizations); err != nil {
		return models.Template{}, err
	}

	clone := source.Clone(name, subject)
	targetScope := ApplyWorkspaceScope(target, models.ResourceVisibilityPrivate)
	mediaCopies, err := c.cloneMediaAssociations(tx, cloneMediaAssociations{Template: associations}, targetScope)
	if err != nil {
		return models.Template{}, err
	}
	sourceNames := make(map[int]string, len(associations))
	for _, association := range associations {
		if association.MediaID.Valid {
			sourceNames[int(association.MediaID.Int)] = association.Filename
		}
	}
	clone.Body = string(rewriteMediaReferences([]byte(clone.Body), mediaCopies, sourceNames))
	if clone.BodySource.Valid {
		clone.BodySource.String = string(rewriteMediaReferences([]byte(clone.BodySource.String), mediaCopies, sourceNames))
	}

	var newID int
	if err := tx.Get(&newID, `
		INSERT INTO templates (
			name, type, subject, body, body_source, is_default,
			organization_id, owner_user_id, original_owner_user_id, visibility
		) VALUES ($1, $2, $3, $4, $5, FALSE, $6, $7, $8, 'private')
		RETURNING id`, clone.Name, clone.Type, clone.Subject, clone.Body, clone.BodySource,
		targetScope.OrganizationID, targetScope.OwnerUserID, targetScope.OriginalOwnerUserID); err != nil {
		return models.Template{}, workspaceQueryError("creating template clone", err)
	}
	for _, association := range associations {
		if err := insertMediaAssociation(tx, "template_media", "template_id", newID, association, mediaCopies); err != nil {
			return models.Template{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.Template{}, workspaceQueryError("committing template clone", err)
	}
	return c.GetWorkspaceTemplate(target, newID, false)
}

func mediaAssociationSourceNames(associations cloneMediaAssociations, copies map[int]int) map[int]string {
	out := make(map[int]string, len(associations.Campaign)+len(associations.Template))
	for _, group := range [][]mediaAssociation{associations.Campaign, associations.Template} {
		for _, association := range group {
			if association.MediaID.Valid {
				if _, ok := copies[int(association.MediaID.Int)]; ok {
					out[int(association.MediaID.Int)] = association.Filename
				}
			}
		}
	}
	return out
}

// cloneCampaignMedia snapshots every still-present media row associated with
// the source campaign or its template. It remains as a small compatibility
// wrapper for package-local callers; the public clone path first locks all
// associations and uses cloneMediaAssociations directly.
func (c *Core) cloneCampaignMedia(tx *sqlx.Tx, campaignID int, templateID null.Int, target models.ResourceScope) (map[int]int, error) {
	associations, err := c.lockCampaignCloneMedia(tx, campaignID, templateID)
	if err != nil {
		return nil, err
	}
	return c.cloneMediaAssociations(tx, associations, target)
}

func (c *Core) cloneMediaAssociations(tx *sqlx.Tx, associations cloneMediaAssociations, target models.ResourceScope) (map[int]int, error) {
	ids := make([]int, 0, len(associations.Campaign)+len(associations.Template))
	for _, group := range [][]mediaAssociation{associations.Campaign, associations.Template} {
		for _, association := range group {
			if association.MediaID.Valid {
				ids = append(ids, int(association.MediaID.Int))
			}
		}
	}
	ids = uniqueMutationIDs(ids)
	sort.Ints(ids)
	clones := make(map[int]int, len(ids))
	for _, sourceID := range ids {
		newID, err := cloneMediaRecord(tx, sourceID, target)
		if err != nil {
			return nil, err
		}
		clones[sourceID] = newID
	}
	return clones, nil
}

// beginCloneTransaction discovers all source-side organization references,
// then locks the source workspace(s) and destination workspace in one stable
// order. The discovery query is intentionally repeated after the source rows
// are locked; if a concurrent operation introduced a new organization
// reference, the clone aborts rather than copying a partially changed graph.
func (c *Core) beginCloneTransaction(resource string, sourceID int, sourceAccess, target models.WorkspaceAccess) (*sqlx.Tx, map[int]string, error) {
	if sourceID < 1 || sourceAccess.UserID < 1 || target.UserID < 1 {
		return nil, nil, echo.NewHTTPError(http.StatusBadRequest, "invalid clone workspace or resource")
	}
	tx, err := c.db.BeginTxx(context.Background(), nil)
	if err != nil {
		return nil, nil, workspaceQueryError("starting workspace clone", err)
	}
	rollback := func(e error) (*sqlx.Tx, map[int]string, error) {
		_ = tx.Rollback()
		return nil, nil, e
	}

	var sourceOrganizations []int
	switch resource {
	case resourceCampaigns:
		sourceOrganizations, err = c.campaignCloneOrganizationIDs(tx, sourceID)
	case resourceTemplates:
		sourceOrganizations, err = c.templateCloneOrganizationIDs(tx, sourceID)
	default:
		err = fmt.Errorf("unsupported clone resource %q", resource)
	}
	if err != nil {
		return rollback(workspaceQueryError("resolving clone workspaces", err))
	}

	orgIDs := append([]int(nil), sourceOrganizations...)
	if sourceAccess.OrganizationID > 0 {
		orgIDs = append(orgIDs, sourceAccess.OrganizationID)
	}
	if target.OrganizationID > 0 {
		orgIDs = append(orgIDs, target.OrganizationID)
	}
	statuses, err := c.lockWorkspaceOrganizations(tx, orgIDs)
	if err != nil {
		return rollback(err)
	}
	if target.Archived || (target.OrganizationID > 0 && statuses[target.OrganizationID] != models.OrganizationStatusActive) {
		return rollback(workspaceMutationError())
	}
	if target.OrganizationID > 0 && !target.PlatformAdmin && !c.workspaceMembershipActive(tx, target.OrganizationID, target.UserID) {
		return rollback(workspaceMutationError())
	}
	if sourceAccess.Archived {
		return rollback(workspaceMutationError())
	}
	if sourceAccess.OrganizationID > 0 && !sourceAccess.PlatformAdmin {
		if statuses[sourceAccess.OrganizationID] != models.OrganizationStatusActive ||
			!c.workspaceMembershipActive(tx, sourceAccess.OrganizationID, sourceAccess.UserID) {
			return rollback(workspaceMutationError())
		}
	}

	locked := make(map[int]string, len(statuses))
	for id, status := range statuses {
		locked[id] = status
	}
	return tx, locked, nil
}

func (c *Core) campaignCloneOrganizationIDs(tx *sqlx.Tx, campaignID int) ([]int, error) {
	var ids []int
	err := tx.Select(&ids, `
		SELECT DISTINCT organization_id FROM (
			SELECT c.organization_id
			FROM campaigns c WHERE c.id = $1
			UNION ALL
			SELECT t.organization_id
			FROM campaigns c JOIN templates t ON t.id = c.template_id
			WHERE c.id = $1
			UNION ALL
			SELECT m.organization_id
			FROM campaigns c
			JOIN campaign_media cm ON cm.campaign_id = c.id
			JOIN media m ON m.id = cm.media_id
			WHERE c.id = $1
			UNION ALL
			SELECT m.organization_id
			FROM campaigns c
			JOIN templates t ON t.id = c.template_id
			JOIN template_media tm ON tm.template_id = t.id
			JOIN media m ON m.id = tm.media_id
			WHERE c.id = $1
		) refs
		WHERE organization_id IS NOT NULL
		ORDER BY organization_id`, campaignID)
	if err != nil {
		return nil, err
	}
	ids = uniqueMutationIDs(ids)
	sort.Ints(ids)
	return ids, nil
}

func (c *Core) templateCloneOrganizationIDs(tx *sqlx.Tx, templateID int) ([]int, error) {
	var ids []int
	err := tx.Select(&ids, `
		SELECT DISTINCT organization_id FROM (
			SELECT t.organization_id FROM templates t WHERE t.id = $1
			UNION ALL
			SELECT m.organization_id
			FROM template_media tm JOIN media m ON m.id = tm.media_id
			WHERE tm.template_id = $1
		) refs
		WHERE organization_id IS NOT NULL
		ORDER BY organization_id`, templateID)
	if err != nil {
		return nil, err
	}
	ids = uniqueMutationIDs(ids)
	sort.Ints(ids)
	return ids, nil
}

func (c *Core) lockCloneResourceScope(tx *sqlx.Tx, resource string, id int, organizationStatuses map[int]string) (models.ResourceScope, error) {
	table, ok := workspaceResourceTables[resource]
	if !ok {
		return models.ResourceScope{}, workspaceMutationError()
	}
	var scope models.ResourceScope
	err := tx.Get(&scope, fmt.Sprintf(`
		SELECT organization_id, owner_user_id, original_owner_user_id,
			visibility, transfer_pending_at
		FROM %s WHERE id = $1 FOR UPDATE`, table), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return scope, ErrNotFound
		}
		return scope, workspaceQueryError("locking clone source", err)
	}
	if scope.OrganizationID.Valid {
		status, ok := organizationStatuses[int(scope.OrganizationID.Int)]
		if !ok {
			// The source changed workspace between discovery and locking. Returning
			// a conflict is safer than acquiring an additional lock out of order.
			return scope, workspaceMutationError()
		}
		scope.OrganizationArchived = status != models.OrganizationStatusActive
	}
	return scope, nil
}

func (c *Core) ensureCloneRelatedOrganizations(tx *sqlx.Tx, resource string, sourceID int, locked map[int]string) error {
	var ids []int
	var err error
	switch resource {
	case resourceCampaigns:
		ids, err = c.campaignCloneOrganizationIDs(tx, sourceID)
	case resourceTemplates:
		ids, err = c.templateCloneOrganizationIDs(tx, sourceID)
	default:
		return workspaceMutationError()
	}
	if err != nil {
		return workspaceQueryError("checking clone workspace references", err)
	}
	for _, id := range ids {
		if _, ok := locked[id]; !ok {
			return workspaceMutationError()
		}
		if locked[id] != models.OrganizationStatusActive {
			return workspaceMutationError()
		}
	}
	return nil
}

func (c *Core) getCampaignCloneSourceTx(tx *sqlx.Tx, id int) (models.Campaign, error) {
	var source models.Campaign
	// GetCampaign intentionally selects campaigns.* because the regular read
	// path scans a Campaigns slice from the database's Unsafe handle.  A
	// transaction's Stmtx wrapper otherwise uses sqlx's safe mapper and rejects
	// legacy progress columns (max_subscriber_id/last_subscriber_id) that are
	// not exposed on models.Campaign.  Preserve the same scan semantics inside
	// the clone transaction while the row lock is held.
	if err := tx.Stmtx(c.q.GetCampaign).Unsafe().Get(&source, id, nil, nil, campaignTplDefault); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return source, ErrNotFound
		}
		return source, workspaceQueryError("reading campaign clone source", err)
	}
	if source.Tags == nil {
		source.Tags = []string{}
	}
	return source, nil
}

func (c *Core) getTemplateCloneSourceTx(tx *sqlx.Tx, id int) (models.Template, error) {
	var templates []models.Template
	if err := tx.Stmtx(c.q.GetTemplates).Select(&templates, id, false, ""); err != nil {
		return models.Template{}, workspaceQueryError("reading template clone source", err)
	}
	if len(templates) == 0 {
		return models.Template{}, ErrNotFound
	}
	return templates[0], nil
}

func (c *Core) lockTemplateCloneMedia(tx *sqlx.Tx, templateID int) ([]mediaAssociation, error) {
	var refs []mediaAssociation
	if err := tx.Select(&refs, `
		SELECT media_id, filename FROM template_media
		WHERE template_id = $1 ORDER BY media_id NULLS LAST, filename
		FOR UPDATE`, templateID); err != nil {
		return nil, workspaceQueryError("locking template media", err)
	}
	if err := c.lockCloneMediaRows(tx, refs); err != nil {
		return nil, err
	}
	if err := c.validateTemplateCloneMedia(tx, templateID); err != nil {
		return nil, err
	}
	return refs, nil
}

func (c *Core) lockCampaignCloneMedia(tx *sqlx.Tx, campaignID int, templateID null.Int) (cloneMediaAssociations, error) {
	var out cloneMediaAssociations
	if err := tx.Select(&out.Campaign, `
		SELECT media_id, filename FROM campaign_media
		WHERE campaign_id = $1 ORDER BY media_id NULLS LAST, filename
		FOR UPDATE`, campaignID); err != nil {
		return out, workspaceQueryError("locking campaign media", err)
	}
	if templateID.Valid && templateID.Int > 0 {
		if err := tx.Select(&out.Template, `
			SELECT media_id, filename FROM template_media
			WHERE template_id = $1 ORDER BY media_id NULLS LAST, filename
			FOR UPDATE`, int(templateID.Int)); err != nil {
			return out, workspaceQueryError("locking campaign template media", err)
		}
	}
	refs := append(append([]mediaAssociation{}, out.Campaign...), out.Template...)
	if err := c.lockCloneMediaRows(tx, refs); err != nil {
		return out, err
	}
	if err := c.validateCampaignCloneMedia(tx, campaignID, templateID); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Core) lockCloneMediaRows(tx *sqlx.Tx, refs []mediaAssociation) error {
	ids := make([]int, 0, len(refs))
	for _, ref := range refs {
		if ref.MediaID.Valid {
			ids = append(ids, int(ref.MediaID.Int))
		}
	}
	ids = uniqueMutationIDs(ids)
	sort.Ints(ids)
	if len(ids) == 0 {
		return nil
	}
	var found []int
	if err := tx.Select(&found, `
		SELECT id FROM media WHERE id = ANY($1::INT[])
		ORDER BY id FOR UPDATE`, pq.Array(ids)); err != nil {
		return workspaceQueryError("locking clone media rows", err)
	}
	if len(found) != len(ids) {
		return workspaceMutationError()
	}
	return nil
}

// validateTemplateCloneMedia makes the media graph part of the template copy
// authorization decision. A shared/global template may deliberately carry a
// private image owned by its author, but an association to a different
// organization (or a transfer-pending/deleted binary) is rejected instead of
// being copied as an orphaned filename row.
func (c *Core) validateTemplateCloneMedia(tx *sqlx.Tx, templateID int) error {
	var invalid int
	if err := tx.Get(&invalid, `
		SELECT COUNT(*)
		FROM template_media tm
		LEFT JOIN templates t ON t.id = tm.template_id
		LEFT JOIN media m ON m.id = tm.media_id
		WHERE tm.template_id = $1
		  AND tm.media_id IS NOT NULL
		  AND NOT COALESCE((
			m.id IS NOT NULL
			AND m.transfer_pending_at IS NULL
			AND (
				m.visibility = 'global'
				OR (
					m.organization_id IS NOT DISTINCT FROM t.organization_id
					AND (m.owner_user_id = t.owner_user_id OR m.visibility = 'organization')
				)
			)
		  ), FALSE)`, templateID); err != nil {
		return workspaceQueryError("validating template clone media", err)
	}
	if invalid > 0 {
		return workspaceMutationError()
	}
	return nil
}

// validateCampaignCloneMedia applies the same graph check to campaign-owned
// media and to media inherited from the campaign's template. The latter is
// intentionally evaluated against the template's owner, allowing private
// author media in a published template without allowing arbitrary private
// media IDs to hitchhike on a clone.
func (c *Core) validateCampaignCloneMedia(tx *sqlx.Tx, campaignID int, templateID null.Int) error {
	var invalid int
	if err := tx.Get(&invalid, `
		SELECT COUNT(*)
		FROM campaign_media cm
		JOIN campaigns c ON c.id = cm.campaign_id
		LEFT JOIN media m ON m.id = cm.media_id
		WHERE cm.campaign_id = $1
		  AND cm.media_id IS NOT NULL
		  AND NOT COALESCE((
			m.id IS NOT NULL
			AND m.transfer_pending_at IS NULL
			AND (
				m.visibility = 'global'
				OR (
					m.organization_id IS NOT DISTINCT FROM c.organization_id
					AND (m.owner_user_id = c.owner_user_id OR m.visibility = 'organization')
				)
			)
		  ), FALSE)`, campaignID); err != nil {
		return workspaceQueryError("validating campaign clone media", err)
	}
	if invalid > 0 {
		return workspaceMutationError()
	}
	if !templateID.Valid || templateID.Int < 1 {
		return nil
	}
	return c.validateTemplateCloneMedia(tx, int(templateID.Int))
}

// validateCampaignCloneTemplate confirms that the saved template is a
// legitimate dependency of the campaign being copied. A public campaign may
// intentionally expose a private template snapshot through its payload, but
// a forged cross-organization template_id must never become a route for
// copying another user's template or media.
func (c *Core) validateCampaignCloneTemplate(tx *sqlx.Tx, campaignID, templateID int) error {
	var valid bool
	if err := tx.Get(&valid, `
		SELECT EXISTS(
			SELECT 1
			FROM campaigns c
			JOIN templates t ON t.id = $2
			LEFT JOIN organizations co ON co.id = c.organization_id
			LEFT JOIN organizations to_org ON to_org.id = t.organization_id
			WHERE c.id = $1
			  AND c.transfer_pending_at IS NULL
			  AND (c.organization_id IS NULL OR co.status = 'active')
			  AND t.transfer_pending_at IS NULL
			  AND (t.organization_id IS NULL OR to_org.status = 'active')
			  AND (
				t.visibility = 'global'
				OR (
					t.organization_id IS NOT DISTINCT FROM c.organization_id
					AND (t.owner_user_id = c.owner_user_id OR t.visibility = 'organization')
				)
			  )
		)`, campaignID, templateID); err != nil {
		return workspaceQueryError("validating campaign clone template", err)
	}
	if !valid {
		return workspaceMutationError()
	}
	return nil
}

func cloneMediaRecord(tx *sqlx.Tx, sourceID int, target models.ResourceScope) (int, error) {
	uu, err := uuid.NewV4()
	if err != nil {
		return 0, err
	}
	var newID int
	if err := tx.Get(&newID, `
		INSERT INTO media (
			uuid, provider, filename, content_type, thumb, meta,
			organization_id, owner_user_id, original_owner_user_id, visibility
		)
		SELECT $2, provider, filename, content_type, thumb, meta,
			$3, $4, $5, 'private'
		FROM media WHERE id = $1
		RETURNING id`, sourceID, uu, target.OrganizationID, target.OwnerUserID, target.OriginalOwnerUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, workspaceMutationError()
		}
		return 0, workspaceQueryError("copying media", err)
	}
	return newID, nil
}

// rewriteTemplateMediaReferencesTx updates a template body after one or more
// referenced media rows have been copied. It is used by personal migrations
// and organization template hand-offs in addition to the public clone path.
func (c *Core) rewriteTemplateMediaReferencesTx(tx *sqlx.Tx, templateID int, copies map[int]int) error {
	if templateID < 1 || len(copies) == 0 {
		return nil
	}
	var row struct {
		Body       string      `db:"body"`
		BodySource null.String `db:"body_source"`
	}
	if err := tx.Get(&row, `SELECT body, body_source FROM templates WHERE id = $1 FOR UPDATE`, templateID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return workspaceMutationError()
		}
		return workspaceQueryError("reading template media body", err)
	}
	newBody := rewriteProtectedMediaReferences([]byte(row.Body), copies)
	newSource := row.BodySource
	if row.BodySource.Valid {
		newSource.String = string(rewriteProtectedMediaReferences([]byte(row.BodySource.String), copies))
	}
	if string(newBody) == row.Body && newSource == row.BodySource {
		return nil
	}
	if _, err := tx.Exec(`
		UPDATE templates SET body = $2, body_source = $3, updated_at = NOW()
		WHERE id = $1`, templateID, string(newBody), newSource); err != nil {
		return workspaceQueryError("rewriting template media references", err)
	}
	return nil
}

func insertMediaAssociation(tx *sqlx.Tx, table, parentColumn string, parentID int, source mediaAssociation, copies map[int]int) error {
	if table != "campaign_media" && table != "template_media" {
		return echo.NewHTTPError(http.StatusInternalServerError, "unknown media association")
	}
	if parentColumn != "campaign_id" && parentColumn != "template_id" {
		return echo.NewHTTPError(http.StatusInternalServerError, "unknown media association parent")
	}
	var mediaID any
	if source.MediaID.Valid {
		copiedID, ok := copies[int(source.MediaID.Int)]
		if !ok {
			// A valid source media reference must have an independent binary in
			// the destination. Writing a filename-only/NULL association here would
			// let a clone succeed while its body still points at a missing image.
			// Return a conflict so the surrounding transaction rolls back the
			// campaign/template snapshot atomically.
			return workspaceMutationError()
		}
		mediaID = copiedID
	}
	stmt := "INSERT INTO " + table + " (" + parentColumn + ", media_id, filename) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING"
	if _, err := tx.Exec(stmt, parentID, mediaID, source.Filename); err != nil {
		return workspaceQueryError("copying media association", err)
	}
	return nil
}

func cloneCampaignHeaders(headers models.Headers) models.Headers {
	out := make(models.Headers, 0, len(headers))
	for _, header := range headers {
		copyHeader := make(map[string]string, len(header))
		for key, value := range header {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "from", "reply-to", "sender", "return-path":
				continue
			}
			copyHeader[key] = value
		}
		if len(copyHeader) > 0 {
			out = append(out, copyHeader)
		}
	}
	return out
}
