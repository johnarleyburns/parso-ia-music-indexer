package db

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSeedCollectionsContent(t *testing.T) {
	var seeds []seedCollection
	if err := json.Unmarshal(seedCollectionsJSON, &seeds); err != nil {
		t.Fatalf("parse seed_collections.json: %v", err)
	}

	if len(seeds) != 2 {
		t.Fatalf("expected 2 seed collections, got %d", len(seeds))
	}

	want := map[string]string{
		"musopen-free":   "collection:musopen",
		"netlabels-free": "collection:netlabels",
	}

	for _, s := range seeds {
		scope, ok := want[s.ID]
		if !ok {
			t.Errorf("unexpected seed collection id %q", s.ID)
			continue
		}
		delete(want, s.ID)

		if !strings.Contains(s.Query, "mediatype:audio") {
			t.Errorf("%s: query missing mediatype:audio: %q", s.ID, s.Query)
		}
		if !strings.Contains(s.Query, scope) {
			t.Errorf("%s: query missing %q: %q", s.ID, scope, s.Query)
		}
		if strings.Contains(strings.ToLower(s.Query), "licenseurl") {
			t.Errorf("%s: query must not filter by licenseurl (filtering is in-app): %q", s.ID, s.Query)
		}
		if s.Title == "" {
			t.Errorf("%s: missing title", s.ID)
		}
	}

	for id := range want {
		t.Errorf("missing expected seed collection %q", id)
	}
}
