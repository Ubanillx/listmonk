package main

import (
	"reflect"
	"testing"

	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
)

func TestCustomerListTypeGroupAndAddedPoolFilters(t *testing.T) {
	pool := models.CustomerList{
		Name: "Alpha public pool", Type: models.CustomerListTypePool,
		Optin: models.CustomerListOptinSingle, Status: models.CustomerListStatusActive,
		Tags: pq.StringArray{"sales", "eu"},
	}
	group, ok := parseCustomerListTypeGroup("pool")
	if !ok || !group.matches(pool) || group.matches(models.CustomerList{Type: models.CustomerListTypePrivate}) {
		t.Fatal("pool group must contain only pools and allocations")
	}
	if _, ok := parseCustomerListTypeGroup("unknown"); ok {
		t.Fatal("unknown type group must be rejected")
	}
	query := customerListQuery{
		search: "PUBLIC", group: group, optin: "single", status: "active",
		tags: []string{"sales"},
	}
	if !query.matchesAddedPool(pool) {
		t.Fatal("matching cross-workspace pool should remain in the result")
	}
	for _, change := range []func(*customerListQuery){
		func(q *customerListQuery) { q.search = "missing" },
		func(q *customerListQuery) { q.status = "archived" },
		func(q *customerListQuery) { q.optin = "double" },
		func(q *customerListQuery) { q.tags = []string{"missing"} },
	} {
		filtered := query
		change(&filtered)
		if filtered.matchesAddedPool(pool) {
			t.Fatalf("pool must not bypass filter: %+v", filtered)
		}
	}
}

func TestSortCustomerListsMergesPoolAndWorkspaceRows(t *testing.T) {
	lists := []models.CustomerList{
		{Base: models.Base{ID: 3}, Name: "Zulu", CustomerCount: 1},
		{Base: models.Base{ID: 2}, Name: "Alpha", CustomerCount: 9},
		{Base: models.Base{ID: 1}, Name: "Bravo", CustomerCount: 3},
	}
	sortCustomerLists(lists, "name", "ASC")
	if got := []int{lists[0].ID, lists[1].ID, lists[2].ID}; !reflect.DeepEqual(got, []int{2, 1, 3}) {
		t.Fatalf("name order after merging: %v", got)
	}
	sortCustomerLists(lists, "customer_count", "DESC")
	if got := []int{lists[0].ID, lists[1].ID, lists[2].ID}; !reflect.DeepEqual(got, []int{2, 1, 3}) {
		t.Fatalf("count order after merging: %v", got)
	}
}
