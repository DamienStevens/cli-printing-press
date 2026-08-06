package cli

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/solutions"
	"github.com/stretchr/testify/require"
)

// docs/solutions/ is only useful if `recall` can find things in it. `module` and
// `tags` are the retrieval keys, so a doc missing either is unfindable — which is
// the same as unwritten. This is the write-path gate: it runs in the existing CI
// job rather than adding a workflow, because the repo already tests its own
// content this way (see printing_press_skill_test.go).
func TestSolutionDocsCarryRetrievalKeys(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("..", "..", "docs", "solutions")
	docs, errs := solutions.LoadDir(dir)
	for _, err := range errs {
		t.Errorf("solution doc does not parse: %v", err)
	}
	require.NotEmpty(t, docs, "no solution docs found under %s", dir)

	for _, doc := range docs {
		if missing := doc.MissingRequired(); len(missing) > 0 {
			t.Errorf("%s: frontmatter is missing %v — recall cannot find this lesson", doc.Path, missing)
		}
	}
}

// A lesson with no symptoms block can never be checked for recurrence, so it can
// never move off "unmeasurable". This is a floor, not a target: it fails only if
// the share gets worse than it is today, so new lessons carry symptoms without
// forcing a backfill of the twenty that predate the rule.
func TestSolutionDocsSymptomCoverageDoesNotRegress(t *testing.T) {
	t.Parallel()

	// 53 of 73 docs carried a symptoms block when learn-audit was introduced
	// (2026-08-05). Raise this floor as the backlog is filled; never lower it.
	const minWithSymptoms = 53

	dir := filepath.Join("..", "..", "docs", "solutions")
	docs, _ := solutions.LoadDir(dir)
	require.NotEmpty(t, docs)

	withSymptoms := 0
	for _, doc := range docs {
		if len(doc.Symptoms) > 0 {
			withSymptoms++
		}
	}
	require.GreaterOrEqualf(t, withSymptoms, minWithSymptoms,
		"only %d of %d solution docs carry a `symptoms` block; learn-audit cannot measure the rest",
		withSymptoms, len(docs))
}
