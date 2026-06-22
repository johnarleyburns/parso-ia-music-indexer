package db

import "strings"

func ComputePillScore(query, trackTitle, trackTags, albumTitle, albumCreator string) float64 {
	queryWords := extractWords(query)
	if len(queryWords) == 0 {
		return 0
	}

	albumTitleWords := wordSet(extractWords(albumTitle))
	creatorWords := wordSet(extractWords(albumCreator))
	trackTitleWords := wordSet(extractWords(trackTitle))
	tagWords := make(map[string]bool)
	if trackTags != "" {
		for _, tag := range strings.Split(trackTags, ", ") {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag != "" && len(tag) > 2 && !stopwords[tag] {
				tagWords[tag] = true
			}
		}
	}

	const (
		tagWeight   = 0.40
		albumWeight = 0.35
		creatorWeight = 0.15
		trackWeight = 0.10
	)

	var score float64
	for _, qw := range queryWords {
		qw = strings.ToLower(qw)
		if tagWords[qw] {
			score += tagWeight
		}
		if albumTitleWords[qw] {
			score += albumWeight
		}
		if creatorWords[qw] {
			score += creatorWeight
		}
		if trackTitleWords[qw] {
			score += trackWeight
		}
	}

	return score / float64(len(queryWords))
}

func wordSet(words []string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}
