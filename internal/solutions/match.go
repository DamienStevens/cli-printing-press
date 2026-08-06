package solutions

import (
	"sort"
	"strings"
)

// Query is a recall request. Every field is optional; an empty Query matches
// nothing, because "return every lesson" is the same as returning none.
type Query struct {
	Module    string
	Component string
	Tags      []string
	Symptom   string
	Limit     int
}

// Hit is one ranked lesson plus the reason it ranked, so a caller can say which
// lessons apply and which don't without opening every file.
type Hit struct {
	Doc     Doc      `json:"doc"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
}

// Scoring weights. Module is the strongest key because it is the one field every
// solution doc carries and the one an editor always knows before editing.
const (
	scoreModuleExact     = 10
	scoreModuleRelated   = 6
	scoreComponentExact  = 5
	scoreTagOverlap      = 3
	scoreRelatedMention  = 2
	scoreSymptomPerToken = 2
	scoreSymptomCap      = 8
)

// Empty reports whether the query would match on nothing.
func (q Query) Empty() bool {
	return strings.TrimSpace(q.Module) == "" &&
		strings.TrimSpace(q.Component) == "" &&
		len(q.Tags) == 0 &&
		strings.TrimSpace(q.Symptom) == ""
}

// Recall ranks docs against the query. Docs that score zero are dropped.
func Recall(docs []Doc, q Query) []Hit {
	if q.Empty() {
		return nil
	}
	wantTags := map[string]bool{}
	for _, t := range q.Tags {
		if t = normalizeKey(t); t != "" {
			wantTags[t] = true
		}
	}
	symptomTokens := SignalTokens(q.Symptom)

	var hits []Hit
	for _, doc := range docs {
		score := 0
		var reasons []string

		if m := normalizeKey(q.Module); m != "" {
			switch {
			case normalizeKey(doc.Module) == m:
				score += scoreModuleExact
				reasons = append(reasons, "module="+doc.Module)
			case moduleRelated(doc.Module, m):
				score += scoreModuleRelated
				reasons = append(reasons, "module~"+doc.Module)
			}
			for _, rc := range doc.RelatedComponents {
				if normalizeKey(rc) == m || moduleRelated(rc, m) {
					score += scoreRelatedMention
					reasons = append(reasons, "related_components="+rc)
					break
				}
			}
		}

		if c := normalizeKey(q.Component); c != "" && normalizeKey(doc.Component) == c {
			score += scoreComponentExact
			reasons = append(reasons, "component="+doc.Component)
		}

		if len(wantTags) > 0 {
			var matched []string
			for _, t := range doc.Tags {
				if wantTags[normalizeKey(t)] {
					matched = append(matched, t)
				}
			}
			if len(matched) > 0 {
				score += scoreTagOverlap * len(matched)
				reasons = append(reasons, "tags="+strings.Join(matched, ","))
			}
		}

		if len(symptomTokens) > 0 {
			overlap := 0
			for tok := range SignalTokens(strings.Join(append(append([]string{}, doc.Symptoms...), doc.Title), " ")) {
				if symptomTokens[tok] {
					overlap++
				}
			}
			if overlap > 0 {
				s := scoreSymptomPerToken * overlap
				if s > scoreSymptomCap {
					s = scoreSymptomCap
				}
				score += s
				reasons = append(reasons, "symptom-overlap")
			}
		}

		if score > 0 {
			hits = append(hits, Hit{Doc: doc, Score: score, Reasons: reasons})
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		// Newer lessons first when scores tie — a later doc on the same module has
		// usually seen the earlier one.
		if !hits[i].Doc.Date.Equal(hits[j].Doc.Date) {
			return hits[i].Doc.Date.After(hits[j].Doc.Date)
		}
		return hits[i].Doc.Path < hits[j].Doc.Path
	})
	if q.Limit > 0 && len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	return hits
}

func normalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// moduleRelated reports whether two module names name overlapping ground. Module
// values in the corpus mix package paths (`internal/generator`) with logical names
// (`cli-printing-press-generator`), so containment either way is the honest test.
func moduleRelated(a, b string) bool {
	na, nb := normalizeKey(a), normalizeKey(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	return strings.Contains(na, nb) || strings.Contains(nb, na)
}
