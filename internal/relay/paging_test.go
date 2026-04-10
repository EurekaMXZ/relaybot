package relay

import (
	"errors"
	"reflect"
	"testing"
)

func TestPaginateRelayItemsEmptyInput(t *testing.T) {
	pages, err := PaginateRelayItems(nil)
	if err != nil {
		t.Fatalf("PaginateRelayItems() error = %v", err)
	}
	if len(pages) != 0 {
		t.Fatalf("PaginateRelayItems() page count = %d, want 0", len(pages))
	}
}

func TestPaginateRelayItemsGrouped11(t *testing.T) {
	items := makeRelayItems(1, 11, "g1")

	pages, err := PaginateRelayItems(items)
	if err != nil {
		t.Fatalf("PaginateRelayItems() error = %v", err)
	}

	assertPageSizes(t, pages, []int{10, 1})
	assertStableOrder(t, items, pages)
	assertSingleGroupPerPage(t, pages)
}

func TestPaginateRelayItemsGrouped21(t *testing.T) {
	items := makeRelayItems(1, 21, "g1")

	pages, err := PaginateRelayItems(items)
	if err != nil {
		t.Fatalf("PaginateRelayItems() error = %v", err)
	}

	assertPageSizes(t, pages, []int{10, 10, 1})
	assertStableOrder(t, items, pages)
	assertSingleGroupPerPage(t, pages)
}

func TestPaginateRelayItemsSingleFile11(t *testing.T) {
	items := makeRelayItems(1, 11, "")

	pages, err := PaginateRelayItems(items)
	if err != nil {
		t.Fatalf("PaginateRelayItems() error = %v", err)
	}

	assertPageSizes(t, pages, []int{10, 1})
	assertStableOrder(t, items, pages)
	assertSingleGroupPerPage(t, pages)
}

func TestPaginateRelayItemsMixedStream(t *testing.T) {
	items := make([]RelayItem, 0, 27)
	items = append(items, makeRelayItems(1, 2, "g1")...)
	items = append(items, makeRelayItems(3, 11, "")...)
	items = append(items, makeRelayItems(14, 3, "g2")...)
	items = append(items, makeRelayItems(17, 11, "g3")...)

	pages, err := PaginateRelayItems(items)
	if err != nil {
		t.Fatalf("PaginateRelayItems() error = %v", err)
	}

	assertPageSizes(t, pages, []int{2, 10, 1, 3, 10, 1})
	assertStableOrder(t, items, pages)
	assertSingleGroupPerPage(t, pages)
}

func TestPaginateRelayItemsGroupFragmented(t *testing.T) {
	items := []RelayItem{
		{ID: 1, MediaGroupID: "g1"},
		{ID: 2, MediaGroupID: "g2"},
		{ID: 3, MediaGroupID: "g1"},
	}

	pages, err := PaginateRelayItems(items)
	if !errors.Is(err, ErrGroupFragmented) {
		t.Fatalf("PaginateRelayItems() error = %v, want %v", err, ErrGroupFragmented)
	}
	if pages != nil {
		t.Fatalf("PaginateRelayItems() pages = %#v, want nil", pages)
	}
}

func makeRelayItems(startID int64, count int, groupID string) []RelayItem {
	items := make([]RelayItem, count)
	for i := 0; i < count; i++ {
		items[i] = RelayItem{
			ID:           startID + int64(i),
			MediaGroupID: groupID,
		}
	}
	return items
}

func assertPageSizes(t *testing.T, pages [][]RelayItem, want []int) {
	t.Helper()

	if len(pages) != len(want) {
		t.Fatalf("page count = %d, want %d", len(pages), len(want))
	}
	for i := range want {
		if len(pages[i]) != want[i] {
			t.Fatalf("page[%d] size = %d, want %d", i, len(pages[i]), want[i])
		}
		if len(pages[i]) > relayPageSize {
			t.Fatalf("page[%d] size = %d, exceeds %d", i, len(pages[i]), relayPageSize)
		}
	}
}

func assertStableOrder(t *testing.T, items []RelayItem, pages [][]RelayItem) {
	t.Helper()

	want := make([]int64, 0, len(items))
	for _, item := range items {
		want = append(want, item.ID)
	}

	got := make([]int64, 0, len(items))
	for _, page := range pages {
		for _, item := range page {
			got = append(got, item.ID)
		}
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flattened ids = %v, want %v", got, want)
	}
}

func assertSingleGroupPerPage(t *testing.T, pages [][]RelayItem) {
	t.Helper()

	for i, page := range pages {
		if len(page) == 0 {
			continue
		}
		groupID := page[0].MediaGroupID
		for _, item := range page[1:] {
			if item.MediaGroupID != groupID {
				t.Fatalf("page[%d] contains mixed groups: %q and %q", i, groupID, item.MediaGroupID)
			}
		}
	}
}
