package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/solutions"
	"github.com/spf13/cobra"
)

const (
	defaultSolutionsDir = "docs/solutions"
	defaultRetrosDir    = "docs/retros"
)

func newRecallCmd() *cobra.Command {
	var (
		solutionsDir string
		module       string
		component    string
		tags         []string
		symptom      string
		limit        int
		asJSON       bool
		noLog        bool
	)

	cmd := &cobra.Command{
		Use:   "recall",
		Short: "Retrieve settled lessons from docs/solutions/ by module, component, tags, or symptom",
		Long: `Searches the YAML frontmatter of docs/solutions/**/*.md and returns the
lessons that apply to what you are about to change.

docs/solutions/ has always been a store with no read path — the only references
to it in the tree were prose in AGENTS.md and three code comments. recall is that
read path. Run it before editing a documented module and state in the plan which
returned lessons apply and which do not.

Every recall that returns hits is appended to docs/solutions/.recall-log.jsonl,
which is what "learn-audit" reads to tell an unproven lesson from a working one.`,
		Example: `  cli-printing-press recall --module internal/generator
  cli-printing-press recall --module internal/pipeline --tags auth
  cli-printing-press recall --symptom "auth_source reports env for config-file credentials"
  cli-printing-press recall --module internal/generator --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := solutions.Query{
				Module:    module,
				Component: component,
				Tags:      tags,
				Symptom:   symptom,
				Limit:     limit,
			}
			if query.Empty() {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("one of --module, --component, --tags, or --symptom is required")}
			}

			docs, errs := solutions.LoadDir(solutionsDir)
			if len(docs) == 0 {
				if len(errs) > 0 {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("reading %s: %v", solutionsDir, errs[0])}
				}
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("no solution docs found under %s", solutionsDir)}
			}
			for _, err := range errs {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
			}

			hits := solutions.Recall(docs, query)

			if len(hits) > 0 && !noLog {
				paths := make([]string, 0, len(hits))
				for _, h := range hits {
					paths = append(paths, h.Doc.Path)
				}
				entry := solutions.RecallEntry{
					Module:    module,
					Component: component,
					Tags:      tags,
					Symptom:   symptom,
					Hits:      paths,
				}
				if err := solutions.AppendRecall(solutions.DefaultRecallLogPath(solutionsDir), entry); err != nil {
					// A lost counter must never cost the caller a lesson.
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: recall log not written: %v\n", err)
				}
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"query": queryJSON(query), "hits": hits})
			}
			printRecallHits(cmd, hits, query)
			return nil
		},
	}

	cmd.Flags().StringVar(&solutionsDir, "solutions-dir", defaultSolutionsDir, "Directory of solution docs")
	cmd.Flags().StringVar(&module, "module", "", "Module about to be edited (matches `module` and `related_components`)")
	cmd.Flags().StringVar(&component, "component", "", "Component filter (exact match on `component`)")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Tags to match (any overlap)")
	cmd.Flags().StringVar(&symptom, "symptom", "", "Free-text symptom to match against `symptoms` and `title`")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum lessons to return (0 = all)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit JSON")
	cmd.Flags().BoolVar(&noLog, "no-log", false, "Do not append to the recall log")
	return cmd
}

func queryJSON(q solutions.Query) map[string]any {
	out := map[string]any{}
	if q.Module != "" {
		out["module"] = q.Module
	}
	if q.Component != "" {
		out["component"] = q.Component
	}
	if len(q.Tags) > 0 {
		out["tags"] = q.Tags
	}
	if q.Symptom != "" {
		out["symptom"] = q.Symptom
	}
	return out
}

func printRecallHits(cmd *cobra.Command, hits []solutions.Hit, q solutions.Query) {
	out := cmd.OutOrStdout()
	if len(hits) == 0 {
		fmt.Fprintf(out, "No settled lessons match this query.\n")
		return
	}
	fmt.Fprintf(out, "%d settled lesson(s) apply — state in your plan which ones you are honoring and which you are not.\n\n", len(hits))
	for i, h := range hits {
		fmt.Fprintf(out, "%d. %s\n", i+1, h.Doc.Title)
		fmt.Fprintf(out, "   %s\n", h.Doc.Path)
		meta := []string{"module=" + h.Doc.Module}
		if h.Doc.Component != "" {
			meta = append(meta, "component="+h.Doc.Component)
		}
		if !h.Doc.Date.IsZero() {
			meta = append(meta, "date="+h.Doc.Date.Format("2006-01-02"))
		}
		if h.Doc.Severity != "" {
			meta = append(meta, "severity="+h.Doc.Severity)
		}
		fmt.Fprintf(out, "   %s\n", strings.Join(meta, "  "))
		fmt.Fprintf(out, "   matched: %s (score %d)\n", strings.Join(h.Reasons, ", "), h.Score)
		if h.Doc.Problem != "" {
			fmt.Fprintf(out, "   %s\n", truncate(collapseWhitespace(h.Doc.Problem), 300))
		}
		fmt.Fprintln(out)
	}
}

func newLearnAuditCmd() *cobra.Command {
	var (
		solutionsDir string
		retrosDir    string
		asJSON       bool
		minTokens    int
		minScore     float64
		failOnRecur  bool
	)

	cmd := &cobra.Command{
		Use:   "learn-audit",
		Short: "Measure whether settled lessons took: recall counts plus symptom recurrence",
		Long: `Cross-references every lesson in docs/solutions/ against every retro in
docs/retros/ dated after it.

The falsifier is mechanical: does a lesson's ` + "`symptoms`" + ` block reappear in a
retro written after that lesson merged? Four verdicts —

  recurrence-candidate  the symptom appears to have recurred — UNCONFIRMED
  working               recalled at least once, no candidate
  unproven              never recalled, no candidate
  unmeasurable          the doc carries no symptoms block, so nothing can be checked

A candidate is not a verdict. Token overlap cannot tell a real recurrence from two
findings that share vocabulary, so every candidate prints the exact shared tokens
and must be confirmed against the retro by a reader before anyone acts on it.
"unmeasurable" is reported and never counted as a pass.`,
		Example: `  cli-printing-press learn-audit
  cli-printing-press learn-audit --json
  cli-printing-press learn-audit --fail-on-recurrence`,
		RunE: func(cmd *cobra.Command, args []string) error {
			docs, docErrs := solutions.LoadDir(solutionsDir)
			if len(docs) == 0 {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("no solution docs found under %s", solutionsDir)}
			}
			retros, retroErrs := solutions.LoadRetros(retrosDir)
			for _, err := range append(docErrs, retroErrs...) {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
			}

			counts, err := solutions.ReadRecallCounts(solutions.DefaultRecallLogPath(solutionsDir))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: recall log unreadable: %v\n", err)
			}

			opts := solutions.DefaultAuditOptions()
			if minTokens > 0 {
				opts.MinTokens = minTokens
			}
			if minScore > 0 {
				opts.MinScore = minScore
			}
			findings := solutions.Audit(docs, retros, counts, opts)
			summary := solutions.Summarize(findings)

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(map[string]any{
					"summary":     summary,
					"retro_count": len(retros),
					"findings":    findings,
				}); err != nil {
					return err
				}
			} else {
				printLearnAudit(cmd, findings, summary, len(retros))
			}

			if failOnRecur && summary.Candidates > 0 {
				return &ExitError{
					Code:   ExitGenerationError,
					Err:    fmt.Errorf("%d recurrence candidate(s) found", summary.Candidates),
					Silent: asJSON,
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&solutionsDir, "solutions-dir", defaultSolutionsDir, "Directory of solution docs")
	cmd.Flags().StringVar(&retrosDir, "retros-dir", defaultRetrosDir, "Directory of retro docs")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit JSON")
	cmd.Flags().IntVar(&minTokens, "min-tokens", 0, "Distinct shared signal tokens required for a recurrence match (default 3)")
	cmd.Flags().Float64Var(&minScore, "min-score", 0, "Rarity-weighted overlap floor for a recurrence match, 0-1 (default 0.34)")
	cmd.Flags().BoolVar(&failOnRecur, "fail-on-recurrence", false, "Exit non-zero when any recurrence candidate is found")
	return cmd
}

func printLearnAudit(cmd *cobra.Command, findings []solutions.Finding, s solutions.Summary, retroCount int) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "learn-audit: %d lesson(s) against %d retro(s)\n\n", s.Total, retroCount)
	fmt.Fprintf(out, "  recurrence-candidate  %3d   symptom may have recurred — UNCONFIRMED\n", s.Candidates)
	fmt.Fprintf(out, "  working               %3d   recalled, no candidate\n", s.Working)
	fmt.Fprintf(out, "  unproven              %3d   never recalled, no candidate\n", s.Unproven)
	fmt.Fprintf(out, "  unmeasurable          %3d   no symptoms block to check\n", s.Unmeasurable)

	var failed []solutions.Finding
	for _, f := range findings {
		if f.Verdict == solutions.VerdictRecurrenceCandidate {
			failed = append(failed, f)
		}
	}
	sort.SliceStable(failed, func(i, j int) bool { return len(failed[i].Recurrences) > len(failed[j].Recurrences) })

	if len(failed) == 0 {
		fmt.Fprintf(out, "\nNo recurrence candidates. This is not yet evidence the lessons held:\n")
		fmt.Fprintf(out, "with %d unproven and %d unmeasurable, most of the corpus has never been\n", s.Unproven, s.Unmeasurable)
		fmt.Fprintf(out, "queried or cannot be checked at all.\n")
		return
	}

	fmt.Fprintf(out, "\nrecurrence candidates — confirm each against the retro before acting:\n")
	for _, f := range failed {
		fmt.Fprintf(out, "\n  %s\n", f.Title)
		fmt.Fprintf(out, "  %s  (module=%s, %s)\n", f.Path, f.Module, f.Date.Format("2006-01-02"))
		for _, r := range f.Recurrences {
			fmt.Fprintf(out, "    ↻ %s (%s)\n", filepath.Base(r.RetroPath), r.RetroDate.Format("2006-01-02"))
			fmt.Fprintf(out, "      symptom: %s\n", truncate(collapseWhitespace(r.Symptom), 160))
			fmt.Fprintf(out, "      shared:  %s (%.0f%%)\n", strings.Join(r.Matched, " "), r.Score*100)
		}
	}
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
