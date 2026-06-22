package db

import (
	"testing"
)

func TestComputePillScoreExactMatch(t *testing.T) {
	score := ComputePillScore(
		"piano",
		"Track Title",                 // trackTitle
		"piano, classical, beethoven", // trackTags
		"The Complete Piano Sonatas",  // albumTitle
		"Beethoven",                   // albumCreator
	)
	if score <= 0 {
		t.Errorf("expected positive score for matching query, got %f", score)
	}
	if score > 1.0 {
		t.Errorf("expected score <= 1.0, got %f", score)
	}
}

func TestComputePillScoreNoMatch(t *testing.T) {
	score := ComputePillScore(
		"violin",
		"Track Title",
		"piano, classical, beethoven",
		"The Complete Piano Sonatas",
		"Beethoven",
	)
	if score != 0 {
		t.Errorf("expected 0 score for no match, got %f", score)
	}
}

func TestComputePillScoreMultiWord(t *testing.T) {
	score := ComputePillScore(
		"piano beethoven",
		"Track Title",
		"piano, classical, beethoven",
		"The Complete Piano Sonatas",
		"Beethoven",
	)
	if score <= 0 {
		t.Errorf("expected positive score, got %f", score)
	}
	if score > 1.0 {
		t.Errorf("expected score <= 1.0, got %f", score)
	}
}

func TestComputePillScoreStopwordsInQuery(t *testing.T) {
	score := ComputePillScore(
		"the piano in the room",
		"Track Title",
		"piano, classical",
		"The Complete Piano Sonatas",
		"Beethoven",
	)
	if score <= 0 {
		t.Errorf("expected positive score when stopwords removed, got %f", score)
	}
}

func TestComputePillScoreEmptyQuery(t *testing.T) {
	score := ComputePillScore(
		"",
		"Track Title",
		"piano, classical",
		"The Complete Sonatas",
		"Beethoven",
	)
	if score != 0 {
		t.Errorf("expected 0 score for empty query, got %f", score)
	}
}

func TestComputePillScoreAlbumTitleMatch(t *testing.T) {
	score := ComputePillScore(
		"sonatas",
		"",
		"",
		"The Complete Piano Sonatas",
		"",
	)
	if score <= 0 {
		t.Errorf("expected positive score for album title match, got %f", score)
	}
}

func TestComputePillScoreCreatorMatch(t *testing.T) {
	score := ComputePillScore(
		"beethoven",
		"",
		"",
		"",
		"Ludwig van Beethoven",
	)
	if score <= 0 {
		t.Errorf("expected positive score for creator match, got %f", score)
	}
}

func TestComputePillScoreTrackTitleMatch(t *testing.T) {
	score := ComputePillScore(
		"allegro",
		"Sonata Allegro Vivace",
		"",
		"",
		"",
	)
	if score <= 0 {
		t.Errorf("expected positive score for track title match, got %f", score)
	}
}

func TestComputePillScoreWeightOrder(t *testing.T) {
	scoreTags := ComputePillScore("test", "", "test", "", "")
	scoreAlbum := ComputePillScore("test", "", "", "test", "")
	scoreCreator := ComputePillScore("test", "", "", "", "test")
	scoreTrack := ComputePillScore("test", "test", "", "", "")

	if scoreTags <= scoreAlbum {
		t.Errorf("expected tag match (%.4f) > album match (%.4f)", scoreTags, scoreAlbum)
	}
	if scoreAlbum <= scoreCreator {
		t.Errorf("expected album match (%.4f) > creator match (%.4f)", scoreAlbum, scoreCreator)
	}
	if scoreCreator <= scoreTrack {
		t.Errorf("expected creator match (%.4f) > track match (%.4f)", scoreCreator, scoreTrack)
	}
}
