package ia

import (
	"encoding/json"
	"testing"
)

func TestRawMessageToSliceString(t *testing.T) {
	raw := json.RawMessage(`"spoken word"`)
	result := rawMessageToSlice(raw)
	if len(result) != 1 || result[0] != "spoken word" {
		t.Errorf("expected [spoken word], got %v", result)
	}
}

func TestRawMessageToSliceArray(t *testing.T) {
	raw := json.RawMessage(`["Non-Music","Education"]`)
	result := rawMessageToSlice(raw)
	if len(result) != 2 || result[0] != "Non-Music" || result[1] != "Education" {
		t.Errorf("expected [Non-Music Education], got %v", result)
	}
}

func TestRawMessageToSliceEmpty(t *testing.T) {
	result := rawMessageToSlice(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestIsMusicContentNonMusicSubject(t *testing.T) {
	m := &AlbumMetadata{Subjects: []string{"Non-Music"}}
	isMusic, reason := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music, got music")
	}
	if reason == "" {
		t.Error("expected reason")
	}
}

func TestIsMusicContentAudiobook(t *testing.T) {
	m := &AlbumMetadata{Subjects: []string{"spoken word", "Audiobook"}}
	isMusic, reason := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music for audiobook, got music")
	}
	if reason == "" {
		t.Error("expected reason")
	}
}

func TestIsMusicContentEducationWithNonMusicGenre(t *testing.T) {
	m := &AlbumMetadata{Genres: []string{"Non-Music", "Education"}}
	isMusic, _ := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music for non-music genre, got music")
	}
}

func TestIsMusicContentMusicSubjects(t *testing.T) {
	m := &AlbumMetadata{Subjects: []string{"Classical", "Folk, World, & Country"}}
	isMusic, _ := IsMusicContent(m)
	if !isMusic {
		t.Errorf("expected music for classical/folk subjects, got non-music")
	}
}

func TestIsMusicContentEmptyMetadata(t *testing.T) {
	m := &AlbumMetadata{}
	isMusic, _ := IsMusicContent(m)
	if !isMusic {
		t.Errorf("expected music for empty metadata, got non-music")
	}
}

func TestIsMusicContentTextsMediatype(t *testing.T) {
	m := &AlbumMetadata{MediaType: "texts"}
	isMusic, _ := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music for texts mediatype")
	}
}

func TestIsMusicContentMoviesMediatype(t *testing.T) {
	m := &AlbumMetadata{MediaType: "movies"}
	isMusic, _ := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music for movies mediatype")
	}
}

func TestIsMusicContentAudioMediatype(t *testing.T) {
	m := &AlbumMetadata{MediaType: "audio"}
	isMusic, _ := IsMusicContent(m)
	if !isMusic {
		t.Errorf("expected music for audio mediatype with no subjects")
	}
}

func TestIsMusicContentLanguageCourseTitle(t *testing.T) {
	m := &AlbumMetadata{Title: "Living Spanish: A Complete Language Course"}
	isMusic, _ := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music for language course title")
	}
}

func TestIsMusicContentLearnMandarinTitle(t *testing.T) {
	m := &AlbumMetadata{Title: "Learn Mandarin Chinese"}
	isMusic, _ := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music for learn mandarin title")
	}
}

func TestIsMusicContentLibrivoxCreator(t *testing.T) {
	m := &AlbumMetadata{Creator: "LibriVox"}
	isMusic, _ := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music for librivox creator")
	}
}

func TestIsMusicContentMusicTitle(t *testing.T) {
	m := &AlbumMetadata{Title: "The Koto Music Of Japan", Subjects: []string{"Classical"}}
	isMusic, _ := IsMusicContent(m)
	if !isMusic {
		t.Errorf("expected music for koto music title")
	}
}

func TestIsMusicContentLanguageCourseDescription(t *testing.T) {
	m := &AlbumMetadata{Description: "This is a learn french audio course with lessons"}
	isMusic, _ := IsMusicContent(m)
	if isMusic {
		t.Errorf("expected non-music for french course description")
	}
}
