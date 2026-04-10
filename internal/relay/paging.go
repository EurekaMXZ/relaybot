package relay

import "errors"

const relayPageSize = 10

var ErrGroupFragmented = errors.New("relay media group is fragmented")

func PaginateRelayItems(items []RelayItem) ([][]RelayItem, error) {
	pages := make([][]RelayItem, 0)
	seenGroups := make(map[string]struct{})

	for index := 0; index < len(items); {
		groupID := items[index].MediaGroupID
		if groupID == "" {
			end := index + 1
			for end < len(items) && items[end].MediaGroupID == "" {
				end++
			}
			appendRelayPages(&pages, items[index:end])
			index = end
			continue
		}

		if _, seen := seenGroups[groupID]; seen {
			return nil, ErrGroupFragmented
		}
		seenGroups[groupID] = struct{}{}

		end := index + 1
		for end < len(items) && items[end].MediaGroupID == groupID {
			end++
		}

		appendRelayPages(&pages, items[index:end])
		index = end
	}

	return pages, nil
}

func appendRelayPages(pages *[][]RelayItem, items []RelayItem) {
	for start := 0; start < len(items); start += relayPageSize {
		end := start + relayPageSize
		if end > len(items) {
			end = len(items)
		}
		*pages = append(*pages, items[start:end])
	}
}
