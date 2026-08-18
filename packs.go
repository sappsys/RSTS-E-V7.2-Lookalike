package rsts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// V7.2 disk designators on a typical 11/70: two letters, optional unit, colon.
// SY: / DK: with no unit is the public structure (here, the system pack).
// A unit number selects a physical drive (DB0:, DL1:, ...).
// Pack IDs (1-6 characters) can be used as logical names once the pack is mounted.

type diskKind struct {
	Name    string
	Media   string
	MaxUnit int
}

var diskKinds = map[string]diskKind{
	"SY":  {Name: "SY", Media: "RP06", MaxUnit: 7},
	"DK":  {Name: "DK", Media: "RK05", MaxUnit: 7},
	"DSK": {Name: "SY", Media: "RP06", MaxUnit: 7},
	"LB":  {Name: "SY", Media: "RP06", MaxUnit: 7},
	"DL":  {Name: "DL", Media: "RL02", MaxUnit: 3},
	"DM":  {Name: "DM", Media: "RK07", MaxUnit: 7},
	"DP":  {Name: "DP", Media: "RP03", MaxUnit: 7},
	"DR":  {Name: "DR", Media: "RM03", MaxUnit: 7},
	"DB":  {Name: "DB", Media: "RP06", MaxUnit: 7},
	"DS":  {Name: "DS", Media: "RS04", MaxUnit: 7},
	"DU":  {Name: "DU", Media: "RA80", MaxUnit: 7},
}

type Pack struct {
	Dev      string `json:"dev"`
	Unit     int    `json:"unit"`
	ID       string `json:"id"`
	Media    string `json:"media"`
	Public   bool   `json:"public"`
	Mounted  bool   `json:"mounted"`
	ReadOnly bool   `json:"readonly"`
	Path     string `json:"path"`
	Init     bool   `json:"init"`
}

func (p *Pack) Designator() string {
	if p.Dev == "SY" {
		return fmt.Sprintf("SY%d:", p.Unit)
	}
	return fmt.Sprintf("%s%d:", p.Dev, p.Unit)
}

func (p *Pack) Flags() string {
	var parts []string
	if p.Public {
		parts = append(parts, "Pub")
	} else if p.Init {
		parts = append(parts, "Pri")
	}
	if p.ReadOnly {
		parts = append(parts, "R-O")
	}
	return strings.Join(parts, ",")
}

type packFile struct {
	Packs []*Pack `json:"packs"`
}

func defaultPacks() []*Pack {
	return []*Pack{
		{Dev: "DB", Unit: 0, ID: "SYSDSK", Media: "RP06", Public: true, Mounted: true, Path: "SY", Init: true},
		{Dev: "DB", Unit: 1, ID: "", Media: "RP06", Path: "DB1"},
		{Dev: "DL", Unit: 0, ID: "", Media: "RL02", Path: "DL0"},
		{Dev: "DL", Unit: 1, ID: "", Media: "RL02", Path: "DL1"},
		{Dev: "DM", Unit: 0, ID: "", Media: "RK07", Path: "DM0"},
	}
}

func splitDeviceToken(tok string) (dev string, unit int, unitSet bool) {
	tok = strings.ToUpper(strings.TrimSpace(tok))
	tok = strings.TrimSuffix(tok, ":")
	if tok == "" {
		return "SY", 0, false
	}
	i := len(tok)
	for i > 0 && unicode.IsDigit(rune(tok[i-1])) {
		i--
	}
	if i == 0 {
		return tok, 0, false
	}
	dev = tok[:i]
	if i < len(tok) {
		unit, _ = strconv.Atoi(tok[i:])
		unitSet = true
	}
	return dev, unit, unitSet
}

func canonicalDiskDev(name string) (diskKind, bool) {
	k, ok := diskKinds[strings.ToUpper(name)]
	if !ok {
		return diskKind{}, false
	}
	if k.Name != name {
		if real, ok := diskKinds[k.Name]; ok {
			return real, true
		}
	}
	return k, true
}

func validPackID(id string) bool {
	id = strings.ToUpper(strings.TrimSpace(id))
	if len(id) < 1 || len(id) > 6 {
		return false
	}
	for _, r := range id {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func (d *Disk) packsPath() string {
	return filepath.Join(d.Root, "packs.json")
}

func (d *Disk) loadPacks() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadPacksLocked()
}

func (d *Disk) loadPacksLocked() error {
	data, err := os.ReadFile(d.packsPath())
	if err != nil {
		if os.IsNotExist(err) {
			d.packs = defaultPacks()
			return d.savePacksLocked()
		}
		return err
	}
	var file packFile
	if err := json.Unmarshal(data, &file); err != nil {
		setAside(d.packsPath(), err)
		d.packs = defaultPacks()
		return d.savePacksLocked()
	}
	if len(file.Packs) == 0 {
		d.packs = defaultPacks()
		return d.savePacksLocked()
	}
	d.packs = file.Packs
	return nil
}

func (d *Disk) savePacksLocked() error {
	data, err := json.MarshalIndent(packFile{Packs: d.packs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(d.packsPath(), append(data, '\n'), 0o644)
}

func (d *Disk) ensureUnitDirs() error {
	for _, p := range d.packs {
		dir := filepath.Join(d.Root, p.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (d *Disk) systemPack() *Pack {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.systemPackLocked()
}

func (d *Disk) systemPackLocked() *Pack {
	for _, p := range d.packs {
		if p != nil && p.Dev == "DB" && p.Unit == 0 {
			return p
		}
	}
	if len(d.packs) > 0 {
		return d.packs[0]
	}
	return &Pack{Dev: "DB", Unit: 0, ID: "SYSDSK", Media: "RP06", Public: true, Mounted: true, Path: "SY", Init: true}
}

func (d *Disk) Packs() []*Pack {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*Pack, len(d.packs))
	copy(out, d.packs)
	return out
}

func (d *Disk) findUnitLocked(dev string, unit int) *Pack {
	kind, ok := canonicalDiskDev(dev)
	if ok {
		dev = kind.Name
	}
	if dev == "SY" {
		if unit == 0 {
			return d.systemPackLocked()
		}
		return nil
	}
	for _, p := range d.packs {
		if p != nil && p.Dev == dev && p.Unit == unit {
			return p
		}
	}
	return nil
}

func (d *Disk) findPackIDLocked(id string) *Pack {
	id = strings.ToUpper(strings.TrimSpace(id))
	for _, p := range d.packs {
		if p != nil && p.Init && strings.EqualFold(p.ID, id) {
			return p
		}
	}
	return nil
}

func (d *Disk) resolvePack(spec FileSpec) (*Pack, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dev := spec.Device
	if dev == "" {
		dev = "SY"
	}
	if kind, ok := canonicalDiskDev(dev); ok {
		unit := spec.Unit
		if !spec.UnitSet {
			if kind.Name == "SY" || kind.Name == "DK" {
				return d.systemPackLocked(), nil
			}
			unit = 0
		}
		p := d.findUnitLocked(kind.Name, unit)
		if p == nil {
			return nil, fsErr("Not a valid device")
		}
		if !p.Mounted {
			return nil, fsErr("Disk not mounted")
		}
		return p, nil
	}
	p := d.findPackIDLocked(dev)
	if p == nil {
		return nil, fsErr("Not a valid device")
	}
	if !p.Mounted {
		return nil, fsErr("Disk not mounted")
	}
	return p, nil
}

// Capacity of the real drives, in 512-byte blocks.
func packCapacity(media string) int {
	switch media {
	case "RP06":
		return 340670
	case "RP04", "RP05":
		return 171796
	case "RP03":
		return 80000
	case "RP02":
		return 40000
	case "RM02", "RM03":
		return 131680
	case "RM05":
		return 500384
	case "RM80":
		return 242606
	case "RL02":
		return 20480
	case "RL01":
		return 10240
	case "RK07":
		return 53790
	case "RK06":
		return 27126
	case "RK05":
		return 4800
	case "RS04":
		return 2048
	case "RS03":
		return 1024
	case "RA80":
		return 237212
	case "RA81":
		return 891072
	case "RA60":
		return 400176
	default:
		return 40000
	}
}

// Cluster size the pack would have been initialized with. A file occupies
// whole clusters, which is why small files still cost several blocks.
func packCluster(media string) int {
	switch media {
	case "RP06", "RP05", "RP04", "RM02", "RM03", "RM05", "RM80",
		"RA80", "RA81", "RA60":
		return 4
	case "RP03", "RP02", "RK07", "RK06":
		return 2
	default:
		return 1
	}
}

// PackUsage walks the pack and returns its capacity and the blocks in use,
// counting each file as the whole clusters it occupies and charging one
// block per account directory for the UFD, the way a RSTS pack does.
func (d *Disk) PackUsage(p *Pack) (capacity, used int) {
	capacity = packCapacity(p.Media)
	if !p.Init {
		return capacity, 0
	}
	cluster := packCluster(p.Media)
	root := d.packRoot(p)
	entries, err := os.ReadDir(root)
	if err != nil {
		return capacity, 0
	}
	used = 1 // MFD
	for _, acct := range entries {
		if !acct.IsDir() {
			continue
		}
		used++ // UFD for the account
		files, err := os.ReadDir(filepath.Join(root, acct.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			folder := filepath.Join(root, acct.Name())
			index := d.loadIndex(folder)
			meta := index[f.Name()]
			if meta.Prot == 0 {
				meta = index[strings.ToUpper(f.Name())]
			}
			cs := meta.Cluster
			if cs < 1 {
				cs = cluster
			}
			n := fileBlocks(info.Size(), cs, meta.Alloc)
			if n == 0 {
				n = cs
			}
			used += n
		}
	}
	if used > capacity {
		used = capacity
	}
	return capacity, used
}

func (d *Disk) packRoot(p *Pack) string {
	if p == nil {
		return d.SY
	}
	return filepath.Join(d.Root, p.Path)
}

func (d *Disk) accountDir(pack *Pack, proj, prog int) (string, error) {
	root := d.packRoot(pack)
	path := filepath.Join(root, fmt.Sprintf("%d,%d", proj, prog))
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func (d *Disk) rejectWrite(spec FileSpec) error {
	pack, err := d.resolvePack(spec)
	if err != nil {
		return err
	}
	if pack.ReadOnly {
		return fsErr("Protection violation")
	}
	return nil
}

func (d *Disk) Initialize(dev string, unit int, packID string, public, privileged bool) error {
	packID = strings.ToUpper(strings.TrimSpace(packID))
	if !validPackID(packID) {
		return fsErr("Illegal pack ID")
	}
	if !privileged {
		return fsErr("Protection violation")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.findUnitLocked(dev, unit)
	if p == nil {
		return fsErr("Not a valid device")
	}
	if p.Dev == "DB" && p.Unit == 0 {
		return fsErr("Protection violation")
	}
	if p.Mounted {
		return fsErr("Disk already mounted")
	}
	if other := d.findPackIDLocked(packID); other != nil && other != p {
		return fsErr("Pack ID already in use")
	}
	dir := filepath.Join(d.Root, p.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p.ID = packID
	p.Init = true
	p.Public = public
	p.Mounted = false
	p.ReadOnly = false
	return d.savePacksLocked()
}

func (d *Disk) Mount(dev string, unit int, packID string, public, readOnly, privileged bool) error {
	packID = strings.ToUpper(strings.TrimSpace(packID))
	if !validPackID(packID) {
		return fsErr("Illegal pack ID")
	}
	if public && !privileged {
		return fsErr("Protection violation")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.findUnitLocked(dev, unit)
	if p == nil {
		return fsErr("Not a valid device")
	}
	if p.Mounted {
		return fsErr("Disk already mounted")
	}
	if !p.Init || p.ID == "" {
		return fsErr("Disk pack not initialized")
	}
	if !strings.EqualFold(p.ID, packID) {
		return fsErr("Pack IDs don't match")
	}
	p.Mounted = true
	p.ReadOnly = readOnly
	if public {
		p.Public = true
	}
	return d.savePacksLocked()
}

func (d *Disk) Dismount(dev string, unit int, packID string, privileged bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.findUnitLocked(dev, unit)
	if p == nil {
		return fsErr("Not a valid device")
	}
	if p.Dev == "DB" && p.Unit == 0 {
		return fsErr("Protection violation")
	}
	if !p.Mounted {
		return fsErr("Disk not mounted")
	}
	if packID != "" && !strings.EqualFold(p.ID, packID) {
		return fsErr("Pack IDs don't match")
	}
	if p.Public && !privileged {
		return fsErr("Protection violation")
	}
	p.Mounted = false
	return d.savePacksLocked()
}
