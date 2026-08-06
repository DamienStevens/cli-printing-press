package solutions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const sampleDoc = `---
title: "AuthHeader must not clobber Load-derived auth provenance"
date: 2026-05-24
category: logic-errors
module: internal/generator
component: authentication
problem_type: logic_error
severity: medium
tags:
  - auth
  - provenance
symptoms:
  - "` + "`auth_source`" + ` reports env for config-file credentials"
related_components:
  - internal/pipeline
---

# AuthHeader must not clobber Load-derived auth provenance

## Problem

Generated CLIs use ` + "`Config.AuthSource`" + ` to explain where credentials came from.

## Resolution

Stop stamping AuthSource in AuthHeader.
`

func TestParseDocReadsFrontmatterAndProblem(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "logic-errors/auth.md", sampleDoc)

	doc, err := ParseDoc(path)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if doc.Module != "internal/generator" {
		t.Errorf("module = %q", doc.Module)
	}
	if doc.Component != "authentication" {
		t.Errorf("component = %q", doc.Component)
	}
	if got := doc.Date.Format("2006-01-02"); got != "2026-05-24" {
		t.Errorf("date = %q", got)
	}
	if len(doc.Tags) != 2 || doc.Tags[0] != "auth" {
		t.Errorf("tags = %v", doc.Tags)
	}
	if len(doc.Symptoms) != 1 {
		t.Fatalf("symptoms = %v", doc.Symptoms)
	}
	if doc.Problem == "" {
		t.Error("Problem paragraph not extracted")
	}
	if len(doc.MissingRequired()) != 0 {
		t.Errorf("MissingRequired = %v, want none", doc.MissingRequired())
	}
}

func TestMissingRequiredFlagsUnfindableDocs(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "x.md", "---\ntitle: \"No keys\"\ndate: 2026-01-01\n---\n\n# No keys\n")
	doc, err := ParseDoc(path)
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	missing := doc.MissingRequired()
	if len(missing) != 2 {
		t.Fatalf("MissingRequired = %v, want module and tags", missing)
	}
}

func TestParseDocRejectsMissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "x.md", "# Just a heading\n")
	if _, err := ParseDoc(path); err == nil {
		t.Fatal("expected an error for a doc with no frontmatter")
	}
}

func TestLoadDirKeepsGoingPastOneBadFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "logic-errors/good.md", sampleDoc)
	writeFile(t, dir, "logic-errors/bad.md", "no frontmatter here\n")

	docs, errs := LoadDir(dir)
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one parse error", errs)
	}
}

func TestRecallRanksModuleAboveTagOnly(t *testing.T) {
	docs := []Doc{
		{Path: "a.md", Title: "A", Module: "internal/generator", Tags: []string{"auth"}},
		{Path: "b.md", Title: "B", Module: "internal/pipeline", Tags: []string{"auth"}},
	}
	hits := Recall(docs, Query{Module: "internal/generator", Tags: []string{"auth"}})
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].Doc.Path != "a.md" {
		t.Errorf("top hit = %s, want a.md", hits[0].Doc.Path)
	}
	if hits[0].Score <= hits[1].Score {
		t.Errorf("module match should outrank tag-only match: %d vs %d", hits[0].Score, hits[1].Score)
	}
}

func TestRecallMatchesRelatedComponents(t *testing.T) {
	docs := []Doc{{Path: "a.md", Module: "publish-workflow", RelatedComponents: []string{"internal/pipeline"}, Tags: []string{"publish"}}}
	hits := Recall(docs, Query{Module: "internal/pipeline"})
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1 via related_components", len(hits))
	}
}

func TestRecallEmptyQueryReturnsNothing(t *testing.T) {
	docs := []Doc{{Path: "a.md", Module: "internal/generator", Tags: []string{"auth"}}}
	if hits := Recall(docs, Query{}); hits != nil {
		t.Fatalf("empty query returned %d hits; returning everything is the same as returning nothing", len(hits))
	}
}

func TestRecallSymptomOverlap(t *testing.T) {
	docs := []Doc{
		{Path: "a.md", Module: "internal/generator", Symptoms: []string{"`auth_source` reports env for config-file credentials"}},
		{Path: "b.md", Module: "internal/generator", Symptoms: []string{"pagination cursor dropped on the second page"}},
	}
	hits := Recall(docs, Query{Symptom: "auth_source reports env when the credential came from the config file"})
	if len(hits) == 0 || hits[0].Doc.Path != "a.md" {
		t.Fatalf("symptom recall failed: %+v", hits)
	}
}

func TestLoadRetrosSkipsUndatedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "2026-04-23-producthunt-retro.md", "# Retro\n")
	writeFile(t, dir, "README.md", "# Not a retro\n")

	retros, errs := LoadRetros(dir)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(retros) != 1 {
		t.Fatalf("retros = %d, want 1", len(retros))
	}
	if got := retros[0].Date.Format("2006-01-02"); got != "2026-04-23" {
		t.Errorf("date = %q", got)
	}
}

// TestAuditFiresOnGenuineRecurrence is the calibration positive: the matcher must
// actually detect a lesson replayed in a later retro. Without this the whole audit
// could be silently dead and would report a clean corpus.
func TestAuditFiresOnGenuineRecurrence(t *testing.T) {
	doc := Doc{
		Path:     "solutions/copydir.md",
		Title:    "Validation must not mutate the source directory",
		Module:   "publish-workflow",
		Date:     time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC),
		Symptoms: []string{"Compiled binaries left in the CLI directory after validation were staged into the library payload by `CopyDir`"},
	}
	retros := []Retro{
		{
			Path: "retros/2026-04-23-later-retro.md",
			Date: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC),
			Body: "## F1. Publish staged a compiled binary again\n\n`CopyDir` copied the freshly built binaries out of the CLI directory into the library payload during validation, exactly as before.",
		},
		{
			Path: "retros/2026-04-01-unrelated-retro.md",
			Date: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Body: "## F1. Pagination cursor was dropped\n\nThe cursor from the first page was not forwarded.",
		},
	}
	findings := Audit([]Doc{doc}, retros, nil, DefaultAuditOptions())
	if len(findings) != 1 {
		t.Fatalf("findings = %d", len(findings))
	}
	if findings[0].Verdict != VerdictRecurrenceCandidate {
		t.Fatalf("verdict = %q, want %q — the matcher does not fire on a real recurrence", findings[0].Verdict, VerdictRecurrenceCandidate)
	}
	if len(findings[0].Recurrences) != 1 {
		t.Fatalf("recurrences = %d, want exactly the later retro", len(findings[0].Recurrences))
	}
	if findings[0].Recurrences[0].RetroPath != "retros/2026-04-23-later-retro.md" {
		t.Errorf("matched the wrong retro: %s", findings[0].Recurrences[0].RetroPath)
	}
}

// TestAuditIgnoresSharedVocabulary is the calibration negative, and it is built
// from a real false positive. The first draft of this matcher reported the
// scorecard-accuracy lesson as recurring in fifteen retros on the strength of
// "grade pass rate runtime scoring verify" — words every retro uses.
func TestAuditIgnoresSharedVocabulary(t *testing.T) {
	doc := Doc{
		Path:     "solutions/scorecard.md",
		Title:    "Scorecard accuracy: broadened pattern matching",
		Module:   "internal/pipeline",
		Date:     time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC),
		Symptoms: []string{"CLI scoring 57/100 (Grade C) despite 91% runtime verify pass rate and 100% live API tests"},
	}
	var retros []Retro
	// Every retro talks about grades, pass rates, scoring and verify — and none of
	// them is this lesson recurring.
	for i := 0; i < 12; i++ {
		retros = append(retros, Retro{
			Path: filepath.Join("retros", "r.md"),
			Date: time.Date(2026, 4, i+1, 0, 0, 0, 0, time.UTC),
			Body: "The CLI scoring run produced a Grade B despite the runtime verify pass rate; live API tests were green. Unrelated finding about pagination.",
		})
	}
	findings := Audit([]Doc{doc}, retros, nil, DefaultAuditOptions())
	if findings[0].Verdict == VerdictRecurrenceCandidate {
		t.Fatalf("matcher fired on shared vocabulary alone: %+v", findings[0].Recurrences)
	}
}

func TestAuditIgnoresRetrosPredatingTheLesson(t *testing.T) {
	doc := Doc{
		Path:     "solutions/copydir.md",
		Module:   "publish-workflow",
		Date:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Symptoms: []string{"Compiled binaries left in the CLI directory were staged into the library payload by `CopyDir`"},
	}
	retros := []Retro{{
		Path: "retros/2026-04-23-earlier-retro.md",
		Date: time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC),
		Body: "`CopyDir` copied compiled binaries out of the CLI directory into the library payload.",
	}}
	findings := Audit([]Doc{doc}, retros, nil, DefaultAuditOptions())
	if findings[0].Verdict == VerdictRecurrenceCandidate {
		t.Fatal("a retro predating the lesson is the observation that produced it, not a recurrence")
	}
}

func TestAuditVerdicts(t *testing.T) {
	docs := []Doc{
		{Path: "no-symptoms.md", Module: "m", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Path: "never-recalled.md", Module: "m", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Symptoms: []string{"pagination cursor dropped"}},
		{Path: "recalled.md", Module: "m", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Symptoms: []string{"pagination cursor dropped"}},
	}
	findings := Audit(docs, nil, map[string]int{"recalled.md": 3}, DefaultAuditOptions())
	want := map[string]Verdict{
		"no-symptoms.md":    VerdictUnmeasurable,
		"never-recalled.md": VerdictUnproven,
		"recalled.md":       VerdictWorking,
	}
	for _, f := range findings {
		if f.Verdict != want[f.Path] {
			t.Errorf("%s: verdict = %q, want %q", f.Path, f.Verdict, want[f.Path])
		}
	}
	if s := Summarize(findings); s.Total != 3 || s.Unmeasurable != 1 || s.Unproven != 1 || s.Working != 1 {
		t.Errorf("summary = %+v", s)
	}
}

func TestRecallLogRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := DefaultRecallLogPath(dir)

	if counts, err := ReadRecallCounts(path); err != nil || len(counts) != 0 {
		t.Fatalf("missing log should read as empty, got %v %v", counts, err)
	}
	for i := 0; i < 2; i++ {
		if err := AppendRecall(path, RecallEntry{Module: "internal/generator", Hits: []string{"a.md", "b.md"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := AppendRecall(path, RecallEntry{Module: "x", Hits: []string{"a.md"}}); err != nil {
		t.Fatal(err)
	}
	counts, err := ReadRecallCounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if counts["a.md"] != 3 || counts["b.md"] != 2 {
		t.Errorf("counts = %v", counts)
	}
}

func TestReadRecallCountsSurvivesCorruptLine(t *testing.T) {
	dir := t.TempDir()
	path := DefaultRecallLogPath(dir)
	body := "{\"hits\":[\"a.md\"]}\nnot json at all\n{\"hits\":[\"a.md\"]}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	counts, err := ReadRecallCounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if counts["a.md"] != 2 {
		t.Errorf("a.md = %d, want 2 — a corrupt line must lose one count, not the history", counts["a.md"])
	}
}
