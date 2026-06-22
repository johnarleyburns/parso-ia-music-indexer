package db

import (
	"strings"
	"testing"
)

func TestGenerateTags(t *testing.T) {
	tags := GenerateTags(
		"78rpm_piano",
		"The Complete Piano Sonatas On Thirteen Discs",
		"Beethoven",
		[]string{"78rpm", "classical", "piano"},
		[]string{"Classical Music", "Piano"},
	)

	mustContain := []string{"piano", "sonatas", "beethoven", "classical", "music", "78rpm"}
	for _, want := range mustContain {
		if !strings.Contains(strings.ToLower(tags), want) {
			t.Errorf("expected tags to contain %q, got %q", want, tags)
		}
	}
}

func TestGenerateTagsRemovesStopwords(t *testing.T) {
	tags := GenerateTags("", "The Of In On Is It At To", "", nil, nil)
	if tags != "" {
		t.Errorf("expected empty tags for stopwords-only title, got %q", tags)
	}
}

func TestGenerateTagsRemovesShortWords(t *testing.T) {
	tags := GenerateTags("ab", "", "", nil, nil)
	if tags != "" {
		t.Errorf("expected empty tags for short-only identifier, got %q", tags)
	}

	tags = GenerateTags("abc", "", "", nil, nil)
	if !strings.Contains(tags, "abc") {
		t.Errorf("expected tag for 3-char word, got %q", tags)
	}
}

func TestGenerateTagsEmptyInputs(t *testing.T) {
	tags := GenerateTags("", "", "", nil, nil)
	if tags != "" {
		t.Errorf("expected empty tags for empty inputs, got %q", tags)
	}
}

func TestGenerateTagsDeduplicates(t *testing.T) {
	tags := GenerateTags(
		"piano_piano",
		"Piano Music",
		"Piano Artist",
		[]string{"piano", "Piano"},
		[]string{"Piano"},
	)

	count := strings.Count(strings.ToLower(tags), "piano")
	if count != 1 {
		t.Errorf("expected 1 occurrence of 'piano', got %d in: %q", count, tags)
	}
}

func TestGenerateTagsSubjectAndGenreExtraction(t *testing.T) {
	tags := GenerateTags(
		"",
		"Symphony No. 5",
		"",
		[]string{"78rpm", "orchestral", "live concert"},
		[]string{"Classical Music", "Orchestral"},
	)

	mustContain := []string{"symphony", "78rpm", "orchestral", "live", "concert", "classical", "music"}
	for _, want := range mustContain {
		if !strings.Contains(strings.ToLower(tags), want) {
			t.Errorf("expected tags to contain %q, got %q", want, tags)
		}
	}
}
