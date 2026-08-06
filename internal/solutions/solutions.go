// Package solutions gives docs/solutions/ a read path.
//
// The directory holds settled lessons — one markdown file per lesson, with YAML
// frontmatter carrying the retrieval keys (module, component, tags, symptoms).
// Until now nothing loaded them: the only references in the tree were prose in
// AGENTS.md and three code comments. A store nobody queries is not a lesson, so
// this package parses the frontmatter into an index (Recall) and measures whether
// the lessons actually took (Audit).
package solutions

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Doc is one settled lesson under docs/solutions/<category>/<slug>.md.
type Doc struct {
	Path              string    `json:"path"`
	Title             string    `json:"title"`
	Date              time.Time `json:"date"`
	Category          string    `json:"category"`
	Module            string    `json:"module"`
	Component         string    `json:"component"`
	ProblemType       string    `json:"problem_type"`
	Severity          string    `json:"severity,omitempty"`
	Tags              []string  `json:"tags"`
	Symptoms          []string  `json:"symptoms,omitempty"`
	RelatedComponents []string  `json:"related_components,omitempty"`
	// Problem is the first paragraph under the `## Problem` heading — the part
	// worth showing at a recall site.
	Problem string `json:"problem,omitempty"`
}

// frontmatter mirrors the YAML block. Fields absent from a given doc stay zero;
// only Module and Tags are load-bearing for retrieval (see RequiredFields).
type frontmatter struct {
	Title             string   `yaml:"title"`
	Date              string   `yaml:"date"`
	Category          string   `yaml:"category"`
	Module            string   `yaml:"module"`
	Component         string   `yaml:"component"`
	ProblemType       string   `yaml:"problem_type"`
	Severity          string   `yaml:"severity"`
	Tags              []string `yaml:"tags"`
	Symptoms          []string `yaml:"symptoms"`
	RelatedComponents []string `yaml:"related_components"`
}

// RequiredFields are the frontmatter keys a solution doc must carry to be
// retrievable. A doc missing them is unfindable, which is the same as unwritten.
var RequiredFields = []string{"module", "tags"}

var frontmatterRe = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n`)

// splitFrontmatter returns the YAML block and the remaining body.
func splitFrontmatter(raw string) (string, string, bool) {
	m := frontmatterRe.FindStringSubmatch(raw)
	if m == nil {
		return "", raw, false
	}
	return m[1], raw[len(m[0]):], true
}

var problemHeadingRe = regexp.MustCompile(`(?m)^##\s+Problem\s*$`)

// extractProblem returns the first non-empty paragraph under `## Problem`.
func extractProblem(body string) string {
	loc := problemHeadingRe.FindStringIndex(body)
	if loc == nil {
		return ""
	}
	rest := body[loc[1]:]
	// Stop at the next heading of any level.
	if end := regexp.MustCompile(`(?m)^#{1,6}\s`).FindStringIndex(rest); end != nil {
		rest = rest[:end[0]]
	}
	for _, para := range strings.Split(strings.TrimSpace(rest), "\n\n") {
		if p := strings.TrimSpace(para); p != "" {
			return p
		}
	}
	return ""
}

// ParseDoc reads one solution doc. A missing or malformed frontmatter block is an
// error — the frontmatter is the schema.
func ParseDoc(path string) (Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, err
	}
	fmBlock, body, ok := splitFrontmatter(string(raw))
	if !ok {
		return Doc{}, fmt.Errorf("%s: no YAML frontmatter block", path)
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
		return Doc{}, fmt.Errorf("%s: frontmatter does not parse: %w", path, err)
	}
	doc := Doc{
		Path:              path,
		Title:             fm.Title,
		Category:          fm.Category,
		Module:            fm.Module,
		Component:         fm.Component,
		ProblemType:       fm.ProblemType,
		Severity:          fm.Severity,
		Tags:              fm.Tags,
		Symptoms:          fm.Symptoms,
		RelatedComponents: fm.RelatedComponents,
		Problem:           extractProblem(body),
	}
	if fm.Date != "" {
		// yaml.v3 hands back an unquoted `2026-05-25` as a string here because the
		// field is typed string; quoted forms arrive the same way.
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(fm.Date)); err == nil {
			doc.Date = t
		}
	}
	return doc, nil
}

// MissingRequired lists the RequiredFields absent from a doc.
func (d Doc) MissingRequired() []string {
	var missing []string
	if strings.TrimSpace(d.Module) == "" {
		missing = append(missing, "module")
	}
	if len(d.Tags) == 0 {
		missing = append(missing, "tags")
	}
	return missing
}

// LoadDir parses every *.md under root (recursively). Parse failures are returned
// alongside the docs that did parse, so a single bad file cannot blind the index.
func LoadDir(root string) ([]Doc, []error) {
	var docs []Doc
	var errs []error
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		doc, perr := ParseDoc(path)
		if perr != nil {
			errs = append(errs, perr)
			return nil
		}
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	return docs, errs
}

// Retro is one observation under docs/retros/. Retros carry no frontmatter, so the
// date comes from the filename prefix (YYYY-MM-DD-<slug>-retro.md).
type Retro struct {
	Path string    `json:"path"`
	Date time.Time `json:"date"`
	Body string    `json:"-"`
}

var retroDateRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)

// LoadRetros parses every *.md under root whose filename starts with a date.
// Files without a date prefix are skipped: without a date there is no "after",
// and "after" is the whole recurrence question.
func LoadRetros(root string) ([]Retro, []error) {
	var retros []Retro
	var errs []error
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		m := retroDateRe.FindStringSubmatch(filepath.Base(path))
		if m == nil {
			return nil
		}
		date, perr := time.Parse("2006-01-02", m[1])
		if perr != nil {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			errs = append(errs, rerr)
			return nil
		}
		retros = append(retros, Retro{Path: path, Date: date, Body: string(raw)})
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	sort.Slice(retros, func(i, j int) bool { return retros[i].Date.Before(retros[j].Date) })
	return retros, errs
}
