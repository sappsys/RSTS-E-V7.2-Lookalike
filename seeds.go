package rsts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// seedFile records what each stock program looked like when it was
// written to the disk. Comparing that against the file now tells an
// untouched sample, which a newer release may replace, from one the user
// has edited, which it must not.
const seedFile = ".seeded.json"

type seeds map[string]string

func loadSeeds(root string) seeds {
	out := seeds{}
	data, err := os.ReadFile(filepath.Join(root, seedFile))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func (s seeds) save(root string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, seedFile), append(data, '\n'), 0o644)
}

func (s seeds) record(path, body string) {
	s[filepath.Base(filepath.Dir(path))+"/"+filepath.Base(path)] = contentHash(body)
}

// replaces reports whether the stock copy of a sample should be written
// over whatever is on disk now.
func (s seeds) replaces(path, body string, proj, prog int) bool {
	onDisk, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	if string(onDisk) == body {
		return false
	}
	// [1,2] holds the notice, the CUSPs and the exerciser. Those are
	// supplied by the system and track the release, so an older disk
	// picks up a newer set.
	if proj == 1 && prog == 2 {
		return true
	}
	key := filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path)
	return s[key] == contentHash(string(onDisk))
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
