package main

import (
	"encoding/json"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A missing translation key is not an error in vue-i18n: it renders the key
// itself, which is how raw strings such as
// "settings.inboundReplies.intentUnsubscribe" end up in the UI. This test scans
// the admin sources for literal $t()/$te() lookups and template-literal prefixes
// and fails when the key does not exist in i18n/en.json.
var (
	reI18nLiteral = regexp.MustCompile("\\$t[se]?\\(\\s*['\"`]([A-Za-z0-9_.\\-]+)['\"`]")
	reI18nDynamic = regexp.MustCompile("\\$t[se]?\\(\\s*`([A-Za-z0-9_.\\-]*)\\$\\{")
)

func TestFrontendI18nKeysExist(t *testing.T) {
	langB, err := os.ReadFile("../i18n/en.json")
	if err != nil {
		t.Fatalf("reading i18n/en.json: %v", err)
	}
	var keys map[string]any
	if err := json.Unmarshal(langB, &keys); err != nil {
		t.Fatalf("parsing i18n/en.json: %v", err)
	}

	// hasKey accepts an exact key, or treats the captured text as a prefix so
	// concatenated and interpolated lookups are covered too.
	hasKey := func(k string) bool {
		if _, ok := keys[k]; ok {
			return true
		}
		if !strings.Contains(k, ".") {
			return false
		}
		for existing := range keys {
			if strings.HasPrefix(existing, k) {
				return true
			}
		}
		return false
	}

	var missing []string
	root := filepath.Join("..", "frontend", "src")
	err = filepath.WalkDir(root, func(path string, d iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".vue", ".js":
		default:
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		src := string(b)
		for _, m := range reI18nLiteral.FindAllStringSubmatch(src, -1) {
			if !hasKey(m[1]) {
				missing = append(missing, fmt.Sprintf("%s: %s", filepath.ToSlash(path), m[1]))
			}
		}
		for _, m := range reI18nDynamic.FindAllStringSubmatch(src, -1) {
			if m[1] != "" && !hasKey(m[1]) {
				missing = append(missing, fmt.Sprintf("%s: %s* (dynamic)", filepath.ToSlash(path), m[1]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("%d frontend translation key(s) missing from i18n/en.json:\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
}
