package main

import (
	"sort"
	"strings"

	"github.com/knadh/listmonk/internal/core"
	"github.com/knadh/listmonk/models"
)

type customerListTypeGroup string

const (
	customerListGroupAll     customerListTypeGroup = ""
	customerListGroupPool    customerListTypeGroup = "pool"
	customerListGroupPrivate customerListTypeGroup = "private"
)

type customerListQuery struct {
	search, typ, optin, status string
	group                      customerListTypeGroup
	tags                       []string
	orderBy, order             string
	offset, limit              int
}

func parseCustomerListTypeGroup(raw string) (customerListTypeGroup, bool) {
	group := customerListTypeGroup(raw)
	switch group {
	case customerListGroupAll, customerListGroupPool, customerListGroupPrivate:
		return group, true
	default:
		return "", false
	}
}

func (group customerListTypeGroup) matches(list models.CustomerList) bool {
	isPool := list.Type == models.CustomerListTypePool || list.Type == models.CustomerListTypeOrgPoolAllocation
	switch group {
	case customerListGroupPool:
		return isPool
	case customerListGroupPrivate:
		return !isPool
	default:
		return true
	}
}

// Cross-workspace pool grants bypass the workspace SQL query. Apply its filters
// to those added rows before permission filtering and pagination.
func (query customerListQuery) matchesAddedPool(list models.CustomerList) bool {
	if !query.group.matches(list) || (query.typ != "" && list.Type != query.typ) ||
		(query.optin != "" && list.Optin != query.optin) ||
		(query.status != "" && list.Status != query.status) ||
		(query.search != "" && !strings.Contains(strings.ToLower(list.Name), strings.ToLower(strings.TrimSpace(query.search)))) {
		return false
	}
	for _, tag := range query.tags {
		found := false
		for _, candidate := range list.Tags {
			if candidate == tag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// The workspace query and pool-grant query each return sorted rows, but the
// merged result needs one order before pagination is applied.
func sortCustomerLists(lists []models.CustomerList, orderBy, order string) {
	ascending := strings.EqualFold(order, core.SortAsc)
	sort.SliceStable(lists, func(i, j int) bool {
		left, right := lists[i], lists[j]
		compare := 0
		switch orderBy {
		case "name":
			compare = strings.Compare(left.Name, right.Name)
		case "status":
			compare = strings.Compare(left.Status, right.Status)
		case "updated_at":
			compare = left.UpdatedAt.Time.Compare(right.UpdatedAt.Time)
		case "customer_count":
			if left.CustomerCount < right.CustomerCount {
				compare = -1
			} else if left.CustomerCount > right.CustomerCount {
				compare = 1
			}
		default:
			compare = left.CreatedAt.Time.Compare(right.CreatedAt.Time)
		}
		if compare == 0 {
			if left.ID < right.ID {
				compare = -1
			} else if left.ID > right.ID {
				compare = 1
			}
		}
		if !ascending {
			compare = -compare
		}
		return compare < 0
	})
}
