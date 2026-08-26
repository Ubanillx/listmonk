package core

import (
	"reflect"
	"testing"
)

func TestUniquePositiveIDs(t *testing.T) {
	got := uniquePositiveIDs([]int{4, 0, -1, 4, 7, 2, 7, 2})
	want := []int{4, 7, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniquePositiveIDs() = %v, want %v", got, want)
	}
}
