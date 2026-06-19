package main

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/johnarleyburns/parso-ia-music-indexer/internal/db"
)

func loadDenylist(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil
	}
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func filterDenylisted(albums []db.AlbumInsert, denylist map[string]bool) []db.AlbumInsert {
	result := make([]db.AlbumInsert, 0, len(albums))
	for _, a := range albums {
		if denylist[a.Identifier] {
			continue
		}
		lower := strings.ToLower(a.Identifier)
		if strings.Contains(lower, "_librivox") || strings.HasPrefix(lower, "librivox") {
			continue
		}
		result = append(result, a)
	}
	return result
}
