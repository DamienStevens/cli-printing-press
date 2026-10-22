package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratedLocalDataPurge compiles a store-backed print and runs
// emitted-code tests for the local-data purge command.
func TestGeneratedLocalDataPurge(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("local-data")
	outputDir := filepath.Join(t.TempDir(), "local-data-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())
	localData, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "local_data.go"))
	require.NoError(t, err)
	require.Contains(t, string(localData), `"mcp:hidden": "true"`, "purge deletes local files and must stay off the MCP surface")

	const runtimeTest = `package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-data-pp-cli/internal/cliutil"
)

func relocateLocalData(t *testing.T) string {
	t.Helper()
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	home := t.TempDir()
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("home override: %v", err)
	}
	t.Cleanup(restore)
	return home
}

func seedLocalFiles(t *testing.T, home string, rel ...string) []string {
	t.Helper()
	var paths []string
	for _, name := range rel {
		path := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

func runLocalDataPurge(t *testing.T, flags *rootFlags, args ...string) string {
	t.Helper()
	cmd := newLocalDataCmd(flags)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(append([]string{"purge"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("purge %v: %v", args, err)
	}
	return out.String()
}

func TestLocalDataPurgeDryRunThenDelete(t *testing.T) {
	home := relocateLocalData(t)
	purged := seedLocalFiles(t, home, "data/data.db", "data/data.db-wal", "data/data-0a1b2c.db", "data/data-0a1b2c.db-shm",
		"cache/http/resources/items/k.json", "state/learn/journal-20261022.jsonl", "state/learn/journal.offset")
	kept := seedLocalFiles(t, home, "data/credentials.toml", "config/config.toml", "state/teach.log")

	out := runLocalDataPurge(t, &rootFlags{})
	if !strings.Contains(out, "Would delete 7 item(s)") {
		t.Fatalf("dry run output = %q", out)
	}
	for _, path := range purged {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("dry run removed %s", path)
		}
	}

	runLocalDataPurge(t, &rootFlags{yes: true})
	for _, path := range purged {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived purge --yes", path)
		}
	}
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("purge removed %s: %v", path, err)
		}
	}
}

func TestLocalDataPurgeOnlyCache(t *testing.T) {
	seeded := seedLocalFiles(t, relocateLocalData(t), "data/data.db", "cache/http/resources/items/k.json")
	store, cached := seeded[0], seeded[1]

	var payload struct {
		DryRun  bool ` + "`json:\"dry_run\"`" + `
		Targets []localDataTarget ` + "`json:\"targets\"`" + `
	}
	out := runLocalDataPurge(t, &rootFlags{yes: true, asJSON: true}, "--only", "cache")
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if payload.DryRun || len(payload.Targets) != 1 || payload.Targets[0].Kind != "cache" {
		t.Fatalf("payload = %+v", payload)
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("--only cache removed the store: %v", err)
	}
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatalf("--only cache kept %s", cached)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "local_data_runtime_test.go"), []byte(runtimeTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestLocalDataPurge", "-count=1")

	minimal := filepath.Join(t.TempDir(), "local-data-min-pp-cli")
	require.NoError(t, New(minimalSpec("local-data-min"), minimal).Generate())
	requireGeneratedCompiles(t, minimal)
}
