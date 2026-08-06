package solutions

import (
	"sort"
	"time"
)

// Verdict is the answer to "did this lesson take?".
type Verdict string

const (
	// VerdictRecurrenceCandidate — the symptom showed up again in a retro dated after
	// the lesson merged. Perfectly stored, and it happened again.
	VerdictRecurrenceCandidate Verdict = "recurrence-candidate"
	// VerdictWorking — the lesson was recalled at least once and has not recurred.
	VerdictWorking Verdict = "working"
	// VerdictUnproven — never recalled, never recurred. May be dead weight, may
	// simply be untested.
	VerdictUnproven Verdict = "unproven"
	// VerdictUnmeasurable — the doc carries no symptoms block, so recurrence
	// cannot be checked at all. Reported, never counted as a pass.
	VerdictUnmeasurable Verdict = "unmeasurable"
)

// Recurrence is one retro that appears to replay a lesson's symptom.
type Recurrence struct {
	RetroPath string    `json:"retro_path"`
	RetroDate time.Time `json:"retro_date"`
	Symptom   string    `json:"symptom"`
	// Matched are the signal tokens shared by the symptom and the retro. They are
	// the evidence: a reader can judge the match without re-running the matcher.
	Matched []string `json:"matched"`
	Score   float64  `json:"score"`
}

// Finding is the audit row for one lesson.
type Finding struct {
	Path         string       `json:"path"`
	Title        string       `json:"title"`
	Module       string       `json:"module"`
	Date         time.Time    `json:"date"`
	DaysSince    int          `json:"days_since"`
	RecallCount  int          `json:"recall_count"`
	Verdict      Verdict      `json:"verdict"`
	Recurrences  []Recurrence `json:"recurrences,omitempty"`
	SymptomCount int          `json:"symptom_count"`
}

// AuditOptions tunes the recurrence matcher.
//
// The defaults are deliberately conservative. A false "recurrence-candidate" is worse
// than a missed one: it sends someone to re-fix a lesson that held, and it makes
// the whole report untrustworthy. The first draft of this matcher matched on
// "grade pass rate runtime scoring verify" — vocabulary every retro shares — and
// reported eight recurrences that were not recurrences at all. MaxCommonShare and
// MinRareTokens exist because of that.
type AuditOptions struct {
	// MinTokens is how many distinct signal tokens a symptom and a retro must
	// share before the match is reported.
	MinTokens int
	// MinRareTokens is how many of those shared tokens must be rare — appearing
	// in no more than RareShare of the retro corpus. A match built entirely from
	// mid-frequency words is repo vocabulary, not evidence.
	MinRareTokens int
	// RareShare is the corpus share at or below which a token counts as rare.
	RareShare float64
	// MaxCommonShare drops tokens appearing in more than this share of retros
	// from the score entirely, numerator and denominator both.
	MaxCommonShare float64
	// MinScore is the floor on the rarity-weighted share of a symptom's scoreable
	// tokens that must appear in the retro, between 0 and 1.
	MinScore float64
	// Now is the reference date for DaysSince. Zero means time.Now().
	Now time.Time
}

// DefaultAuditOptions are the thresholds the report ships with.
func DefaultAuditOptions() AuditOptions {
	return AuditOptions{
		MinTokens:      3,
		MinRareTokens:  2,
		RareShare:      0.15,
		MaxCommonShare: 0.4,
		MinScore:       0.34,
	}
}

// Audit checks every lesson against every retro dated after it.
//
// The falsifier: does a solution doc's symptoms block reappear in a retro dated
// after that doc merged? A doc's date stands in for its merge date — the corpus
// has no merge timestamp in frontmatter, and the two are within a day in practice.
func Audit(docs []Doc, retros []Retro, recallCounts map[string]int, opts AuditOptions) []Finding {
	def := DefaultAuditOptions()
	if opts.MinTokens <= 0 {
		opts.MinTokens = def.MinTokens
	}
	if opts.MinRareTokens <= 0 {
		opts.MinRareTokens = def.MinRareTokens
	}
	if opts.RareShare <= 0 {
		opts.RareShare = def.RareShare
	}
	if opts.MaxCommonShare <= 0 {
		opts.MaxCommonShare = def.MaxCommonShare
	}
	if opts.MinScore <= 0 {
		opts.MinScore = def.MinScore
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	// Rarity is measured against the retro corpus: a token in most retros is
	// vocabulary, not evidence.
	retroTexts := make([]string, 0, len(retros))
	for _, r := range retros {
		retroTexts = append(retroTexts, r.Body)
	}
	df := documentFrequency(retroTexts)
	retroTokens := make([]map[string]bool, len(retros))
	for i, r := range retros {
		retroTokens[i] = SignalTokens(r.Body)
	}

	findings := make([]Finding, 0, len(docs))
	for _, doc := range docs {
		f := Finding{
			Path:         doc.Path,
			Title:        doc.Title,
			Module:       doc.Module,
			Date:         doc.Date,
			RecallCount:  recallCounts[doc.Path],
			SymptomCount: len(doc.Symptoms),
		}
		if !doc.Date.IsZero() {
			f.DaysSince = int(now.Sub(doc.Date).Hours() / 24)
		}

		for _, symptom := range doc.Symptoms {
			tokens := SignalTokens(symptom)
			if len(tokens) == 0 {
				continue
			}
			for i, retro := range retros {
				if !doc.Date.IsZero() && !retro.Date.After(doc.Date) {
					continue
				}
				matched, rare, score := matchSymptom(tokens, retroTokens[i], df, len(retros), opts)
				if len(matched) >= opts.MinTokens && rare >= opts.MinRareTokens && score >= opts.MinScore {
					sort.Strings(matched)
					f.Recurrences = append(f.Recurrences, Recurrence{
						RetroPath: retro.Path,
						RetroDate: retro.Date,
						Symptom:   symptom,
						Matched:   matched,
						Score:     score,
					})
				}
			}
		}

		switch {
		case len(f.Recurrences) > 0:
			f.Verdict = VerdictRecurrenceCandidate
		case len(doc.Symptoms) == 0:
			f.Verdict = VerdictUnmeasurable
		case f.RecallCount > 0:
			f.Verdict = VerdictWorking
		default:
			f.Verdict = VerdictUnproven
		}
		findings = append(findings, f)
	}
	return findings
}

// matchSymptom returns the shared signal tokens, how many of them are rare, and
// the rarity-weighted share of the symptom's scoreable tokens that the retro
// reproduces. Tokens more common than MaxCommonShare are dropped from both the
// numerator and the denominator: they are the corpus's vocabulary, and neither
// their presence nor their absence is evidence about a specific lesson.
func matchSymptom(symptom, retro map[string]bool, df map[string]int, corpusSize int, opts AuditOptions) ([]string, int, float64) {
	var matched []string
	var rare int
	var totalWeight, matchedWeight float64
	for tok := range symptom {
		freq := df[tok]
		share := corpusShare(freq, corpusSize)
		// The share cutoffs need a document-count floor: in a small corpus a token
		// in one of two retros has a 50% share without being vocabulary.
		if freq >= minCommonDocs && share > opts.MaxCommonShare {
			continue
		}
		w := 1 - share
		totalWeight += w
		if retro[tok] {
			matched = append(matched, tok)
			matchedWeight += w
			if freq <= 1 || share <= opts.RareShare {
				rare++
			}
		}
	}
	if totalWeight == 0 {
		return nil, 0, 0
	}
	return matched, rare, matchedWeight / totalWeight
}

// minCommonDocs is the document count below which a token cannot be dismissed as
// corpus vocabulary, however large its share looks.
const minCommonDocs = 3

// corpusShare is the fraction of retros containing a token.
func corpusShare(docFreq, corpusSize int) float64 {
	if corpusSize <= 0 || docFreq <= 0 {
		return 0
	}
	return float64(docFreq) / float64(corpusSize)
}

// Summary counts findings by verdict.
type Summary struct {
	Total        int `json:"total"`
	Candidates   int `json:"recurrence_candidates"`
	Working      int `json:"working"`
	Unproven     int `json:"unproven"`
	Unmeasurable int `json:"unmeasurable"`
}

// Summarize tallies the verdicts.
func Summarize(findings []Finding) Summary {
	s := Summary{Total: len(findings)}
	for _, f := range findings {
		switch f.Verdict {
		case VerdictRecurrenceCandidate:
			s.Candidates++
		case VerdictWorking:
			s.Working++
		case VerdictUnproven:
			s.Unproven++
		case VerdictUnmeasurable:
			s.Unmeasurable++
		}
	}
	return s
}
