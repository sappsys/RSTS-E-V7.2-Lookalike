package rsts

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

// Stock programs seeded onto a new disk.
//
//	cusps/*.BAS          library CUSPs → [1,2]  (see cusp.go)
//	demos/*              [1,9] LIBRARY (COMP, DATA, WHOAMI)
//	samples/<ppn>/*      that account ([1,2] notices, [100,100], [200,200])
//
// Placeholders @@REL@@ and @@LONG@@ are filled at load (SystemRelease,
// SystemLong). WHOAMI.BAC is compiled from WHOAMI.BAS at seed.
//
//go:embed demos samples
var seedFS embed.FS

var samples = loadSeedFiles()

func loadSeedFiles() map[string]map[string]string {
	out := map[string]map[string]string{}
	put := func(ppn, name, body string) {
		if out[ppn] == nil {
			out[ppn] = map[string]string{}
		}
		out[ppn][strings.ToUpper(name)] = applySeedVars(body)
	}
	_ = fs.WalkDir(seedFS, "demos", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := seedFS.ReadFile(p)
		if err != nil {
			return err
		}
		put("1,9", d.Name(), string(data))
		return nil
	})
	_ = fs.WalkDir(seedFS, "samples", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, "samples/")
		dir, name := path.Split(rel)
		ppn := strings.TrimSuffix(dir, "/")
		if ppn == "" {
			return nil
		}
		data, err := seedFS.ReadFile(p)
		if err != nil {
			return err
		}
		put(ppn, name, string(data))
		return nil
	})
	if demos := out["1,9"]; demos != nil {
		if src := demos["WHOAMI.BAS"]; src != "" {
			demos["WHOAMI.BAC"] = src
		}
	}
	return out
}

func applySeedVars(body string) string {
	body = strings.ReplaceAll(body, "@@REL@@", SystemRelease)
	body = strings.ReplaceAll(body, "@@LONG@@", SystemLong)
	return body
}
