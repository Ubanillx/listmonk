package core

import (
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

// EnrichPoolListUnsubscribeCounts adds counters only to already-authorized list
// rows. Its membership and organization boundaries match QueryPoolContacts.
func (c *Core) EnrichPoolListUnsubscribeCounts(lists []models.CustomerList, organizationID int, platformAdmin, poolMaster bool) error {
	ids := make([]int, 0, len(lists))
	for _, list := range lists {
		if list.Type == models.CustomerListTypePool || list.Type == models.CustomerListTypeOrgPoolAllocation {
			ids = append(ids, list.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var counts []struct {
		ID    int `db:"id"`
		Count int `db:"count"`
	}
	if err := c.db.Select(&counts, `
		SELECT l.id, COUNT(DISTINCT ue.contact_id) AS count
		FROM customer_lists l
		LEFT JOIN org_pool_allocations allocation ON allocation.list_id=l.id
		JOIN pool_members pm ON pm.pool_id=CASE WHEN l.type='pool' THEN l.id ELSE allocation.pool_id END
		JOIN org_pool_allocation_exclusions ue ON ue.pool_id=pm.pool_id AND ue.contact_id=pm.contact_id
			AND ue.source='unsubscribe' AND ue.restored_at IS NULL
		WHERE l.id=ANY($1::INT[]) AND (
			(l.type='pool' AND ($3 OR $4 OR (ue.organization_id=$2 AND EXISTS (
				SELECT 1 FROM org_pool_allocation_members sm
				JOIN org_pool_allocations ps ON ps.id=sm.allocation_id
				WHERE ps.pool_id=pm.pool_id AND ps.organization_id=$2
					AND sm.contact_id=pm.contact_id AND sm.status IN ('active','removed')
			)))) OR
			(l.type='org_pool_allocation' AND ue.organization_id=allocation.organization_id
				AND ($3 OR allocation.organization_id=$2) AND EXISTS (
					SELECT 1 FROM org_pool_allocation_members sm
					WHERE sm.allocation_id=allocation.id AND sm.contact_id=pm.contact_id
						AND sm.status IN ('active','removed')
				))
		) GROUP BY l.id`, pq.Array(ids), organizationID, platformAdmin, poolMaster); err != nil {
		return err
	}
	byID := make(map[int]int, len(counts))
	for _, count := range counts {
		byID[count.ID] = count.Count
	}
	for i := range lists {
		if lists[i].Type != models.CustomerListTypePool && lists[i].Type != models.CustomerListTypeOrgPoolAllocation {
			continue
		}
		lists[i].CustomerCounts = models.StringIntMap{}
		if count := byID[lists[i].ID]; count > 0 {
			lists[i].CustomerCounts["unsubscribed"] = count
		}
	}
	return nil
}
