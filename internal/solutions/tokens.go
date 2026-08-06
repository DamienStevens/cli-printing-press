package solutions

import (
	"regexp"
	"strings"
)

// Signal tokens are the part of a symptom string that identifies it: code spans,
// file paths, flags, and identifiers. Prose words are deliberately weak — two
// unrelated findings both say "the CLI returned an error", so matching on prose
// produces a recurrence report that fires on everything and means nothing.

var (
	backtickRe   = regexp.MustCompile("`([^`]+)`")
	wordRe       = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.\-/]*`)
	camelSplitRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

// stopwords are high-frequency English and high-frequency repo vocabulary. A
// token that appears in most retros carries no evidence that a specific lesson
// recurred, so both kinds are excluded.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"from": true, "into": true, "when": true, "then": true, "than": true, "but": true,
	"not": true, "was": true, "were": true, "are": true, "its": true, "has": true,
	"had": true, "have": true, "does": true, "did": true, "can": true, "will": true,
	"would": true, "should": true, "could": true, "which": true, "while": true,
	"they": true, "them": true, "there": true, "their": true, "what": true,
	"only": true, "also": true, "some": true, "each": true, "every": true,
	"even": true, "more": true, "most": true, "other": true, "same": true,
	"still": true, "such": true, "over": true, "under": true, "after": true,
	"before": true, "because": true, "been": true, "being": true, "both": true,
	// Repo vocabulary that appears in nearly every retro and solution doc.
	"cli": true, "api": true, "press": true, "printing": true, "spec": true,
	"code": true, "file": true, "files": true, "test": true, "tests": true,
	"error": true, "errors": true, "value": true, "values": true, "name": true,
	"names": true, "field": true, "fields": true, "command": true, "commands": true,
	"generated": true, "generate": true, "output": true, "input": true,
	"run": true, "runs": true, "case": true, "cases": true, "check": true,
	"checks": true, "fix": true, "fixed": true, "issue": true, "agent": true,
	"user": true, "users": true, "data": true, "json": true, "http": true,
	"request": true, "response": true, "flag": true, "flags": true,
}

// minTokenLen drops one-to-three character fragments ("id", "url", "n"), which
// collide constantly and carry no evidence.
const minTokenLen = 4

// SignalTokens extracts the identifying tokens from a piece of text. Code spans
// inside backticks are the strongest signal, so their atoms are always kept; bare
// prose contributes only tokens that look like identifiers (containing _ . - / or
// camelCase) or that survive the stopword and length filters.
func SignalTokens(text string) map[string]bool {
	out := map[string]bool{}

	// Code spans first, then the same text without the backticks so a symptom that
	// mentions a term both ways is not double-counted.
	for _, m := range backtickRe.FindAllStringSubmatch(text, -1) {
		for tok := range atomize(m[1]) {
			out[tok] = true
		}
	}
	plain := backtickRe.ReplaceAllString(text, " ")
	for _, w := range wordRe.FindAllString(plain, -1) {
		structured := strings.ContainsAny(w, "_./-") || camelSplitRe.MatchString(w)
		for tok := range atomize(w) {
			if structured || !stopwords[tok] {
				out[tok] = true
			}
		}
	}
	return out
}

// atomize lowercases an identifier and splits it into its parts, keeping both the
// whole and the pieces: `auth_source` yields auth_source, auth, source. Matching on
// either end survives the rename that turns auth_source into authSource.
func atomize(s string) map[string]bool {
	out := map[string]bool{}
	s = strings.TrimSpace(s)
	if s == "" {
		return out
	}
	whole := strings.ToLower(camelSplitRe.ReplaceAllString(s, "${1}_${2}"))
	add := func(tok string) {
		tok = strings.Trim(tok, "-_./ ")
		if len(tok) >= minTokenLen && !stopwords[tok] {
			out[tok] = true
		}
	}
	add(strings.ToLower(s))
	add(whole)
	for _, part := range strings.FieldsFunc(whole, func(r rune) bool {
		return r == '_' || r == '.' || r == '-' || r == '/' || r == ' '
	}) {
		add(part)
	}
	return out
}

// documentFrequency counts, for each token, how many of the given texts contain it.
func documentFrequency(texts []string) map[string]int {
	df := map[string]int{}
	for _, t := range texts {
		for tok := range SignalTokens(t) {
			df[tok]++
		}
	}
	return df
}
