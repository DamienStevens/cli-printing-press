package solutions

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// The recall log is a sidecar, never the doc. A merged solution doc is settled
// state — the PR that merged it is the authority grant — so usage counters live
// beside it and can be deleted without losing a lesson.
//
// ponytail: the log is gitignored, so "working" is a per-machine verdict — one
// engineer's recalls do not raise the count for anyone else. That is the right
// trade while this is one repo's read path; if the counter needs to be shared,
// the upgrade is to append to a committed file per author or to ship the counts
// with the retro, not to make every recall write a conflict-prone shared file.

// RecallEntry is one append-only record of a recall that returned hits.
type RecallEntry struct {
	At        time.Time `json:"at"`
	Module    string    `json:"module,omitempty"`
	Component string    `json:"component,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	Symptom   string    `json:"symptom,omitempty"`
	Hits      []string  `json:"hits"`
}

// DefaultRecallLogPath is the sidecar's home, next to the docs it counts.
func DefaultRecallLogPath(solutionsDir string) string {
	return filepath.Join(solutionsDir, ".recall-log.jsonl")
}

// AppendRecall records one recall. A failure to write is returned but is never
// fatal to the recall itself: losing a counter must not cost the caller a lesson.
func AppendRecall(path string, entry RecallEntry) error {
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ReadRecallCounts tallies how many times each doc path has been returned by a
// recall. A missing log is not an error — it means nothing has been recalled yet,
// which is exactly the state this whole change exists to move off of.
func ReadRecallCounts(path string) (map[string]int, error) {
	counts := map[string]int{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return counts, nil
		}
		return counts, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry RecallEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// A corrupt line loses one count, not the whole history.
			continue
		}
		for _, hit := range entry.Hits {
			counts[hit]++
		}
	}
	return counts, scanner.Err()
}
