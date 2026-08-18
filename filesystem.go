package rsts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	blockSize   = 512
	defaultProt = 60
	// V7 compiled default: executable + owner-only (60+64).
	compiledProt = 124
	// Typical public privileged compiled program (64+128+32+8).
	privCompiledProt = 232

	protOwnerRead  = 1
	protOwnerWrite = 2
	protGroupRead  = 4
	protGroupWrite = 8
	protWorldRead  = 16
	protWorldWrite = 32
	protExecutable = 64
	protPrivileged = 128

	bacMagic = "RSTS/E BAC V7\n"
)

type accessOp int

const (
	accLookup accessOp = iota
	accRead
	accWrite
	accExecute
	accDelete
)

type FSError struct{ Msg string }

func (e *FSError) Error() string { return e.Msg }

func fsErr(msg string) error { return &FSError{Msg: msg} }

type FileSpec struct {
	Device   string
	Unit     int
	UnitSet  bool
	Proj     *int
	Prog     *int
	Name     string
	Ext      string
	Wildcard bool
	Prot     int
	ProtSet  bool
	ExtGiven bool
}

func (s FileSpec) DevName() string {
	if s.Device == "" {
		return "SY:"
	}
	if s.UnitSet {
		return fmt.Sprintf("%s%d:", s.Device, s.Unit)
	}
	if _, ok := canonicalDiskDev(s.Device); ok {
		return s.Device + ":"
	}
	return s.Device + ":"
}

func (s FileSpec) Filename() string {
	if s.Ext != "" {
		return s.Name + "." + s.Ext
	}
	return s.Name
}

type FileInfo struct {
	Name     string
	Size     int64
	Prot     int
	Modified time.Time
	Cluster  int
	Alloc    int
	Path     string
}

func (f FileInfo) Blocks() int {
	return fileBlocks(f.Size, f.Cluster, f.Alloc)
}

func (f FileInfo) NamePart() string {
	if i := strings.IndexByte(f.Name, '.'); i >= 0 {
		return f.Name[:i]
	}
	return f.Name
}

func (f FileInfo) ExtPart() string {
	if i := strings.IndexByte(f.Name, '.'); i >= 0 {
		return f.Name[i+1:]
	}
	return ""
}

var fileSpecRe = regexp.MustCompile(`(?s)^\s*(?:([A-Za-z$][A-Za-z0-9$]*):)?(?:\[(\d+)\s*,\s*(\d+)\])?([A-Za-z0-9$._*%-]*)\s*$`)

func splitProt(raw string) (string, int, bool, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndex(raw, "<"); i >= 0 && strings.HasSuffix(raw, ">") {
		inner := strings.TrimSpace(raw[i+1 : len(raw)-1])
		n, err := strconv.Atoi(inner)
		if err != nil || n < 0 || n > 255 {
			return "", 0, false, fsErr("Illegal protection code")
		}
		return strings.TrimSpace(raw[:i]), n, true, nil
	}
	return raw, 0, false, nil
}

func ParseFileSpec(text, defaultExt string) (FileSpec, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return FileSpec{}, fsErr("Illegal file name")
	}
	raw, prot, protSet, err := splitProt(raw)
	if err != nil {
		return FileSpec{}, err
	}
	if strings.HasPrefix(strings.ToUpper(raw), "$") && !strings.HasPrefix(raw, "$:") {
		raw = "[1,2]" + raw[1:]
	}
	m := fileSpecRe.FindStringSubmatch(raw)
	if m == nil {
		return FileSpec{}, fsErr("Illegal file name")
	}
	deviceTok := strings.ToUpper(m[1])
	dev, unit, unitSet := splitDeviceToken(deviceTok)
	if deviceTok == "" {
		dev, unit, unitSet = "SY", 0, false
	}
	if _, ok := canonicalDiskDev(dev); !ok && !validPackID(dev) {
		return FileSpec{}, fsErr("Not a valid device")
	}
	var proj, prog *int
	if m[2] != "" {
		p, _ := strconv.Atoi(m[2])
		q, _ := strconv.Atoi(m[3])
		proj, prog = &p, &q
	}
	nametok := strings.ToUpper(m[4])
	var name, ext string
	wildcard := false
	extGiven := false
	if nametok == "" || nametok == "." || nametok == ".." {
		name, ext = "*", "*"
		wildcard = true
	} else if strings.Contains(nametok, ".") {
		parts := strings.SplitN(nametok, ".", 2)
		name, ext = parts[0], parts[1]
		extGiven = true
		if strings.Contains(ext, ".") {
			return FileSpec{}, fsErr("Illegal file name")
		}
	} else {
		name, ext = nametok, strings.ToUpper(defaultExt)
	}
	if strings.ContainsAny(name, "*?") || strings.ContainsAny(ext, "*?") {
		wildcard = true
	}
	if !wildcard {
		if name != "" && !nameOK(name, 9) {
			return FileSpec{}, fsErr("Illegal file name")
		}
		if ext != "" && !nameOK(ext, 3) {
			return FileSpec{}, fsErr("Illegal file name")
		}
	}
	if name == "" {
		name = "*"
	}
	return FileSpec{
		Device:   dev,
		Unit:     unit,
		UnitSet:  unitSet,
		Proj:     proj,
		Prog:     prog,
		Name:     name,
		Ext:      ext,
		Wildcard: wildcard,
		Prot:     prot,
		ProtSet:  protSet,
		ExtGiven: extGiven,
	}, nil
}

func wrapBAC(payload string) string {
	return bacMagic + payload
}

func unwrapBAC(text string) (string, bool) {
	if strings.HasPrefix(text, bacMagic) {
		return text[len(bacMagic):], true
	}
	return text, false
}

func isPrivCompiled(prot int) bool {
	return prot&protExecutable != 0 && prot&protPrivileged != 0
}

func checkPrivProt(prot int, privileged bool) error {
	if prot < 0 || prot > 255 {
		return fsErr("Illegal protection code")
	}
	if prot&protPrivileged != 0 {
		if !privileged {
			return fsErr("Protection violation")
		}
		if prot&protExecutable == 0 {
			return fsErr("Protection violation")
		}
	}
	return nil
}

func protAllows(prot int, owner, group, privileged bool, op accessOp) bool {
	if privileged {
		return true
	}
	compiled := prot&protExecutable != 0
	switch op {
	case accLookup:
		return true
	case accRead:
		if compiled && !owner {
			return false
		}
		if owner {
			return prot&protOwnerRead == 0
		}
		if group {
			return prot&protGroupRead == 0
		}
		return prot&protWorldRead == 0
	case accWrite, accDelete:
		if owner {
			return prot&protOwnerWrite == 0
		}
		if group {
			return prot&protGroupWrite == 0
		}
		return prot&protWorldWrite == 0
	case accExecute:
		if !compiled {
			return false
		}
		if owner {
			return prot&protOwnerRead == 0
		}
		if group {
			return prot&protGroupRead == 0
		}
		return prot&protWorldRead == 0
	default:
		return false
	}
}

func nameOK(s string, max int) bool {
	if len(s) < 1 || len(s) > max {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '$' {
			return false
		}
	}
	return true
}

func wildMatch(name, pattern string) bool {
	re := "^" + strings.ReplaceAll(strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, `.*`), `\?`, `.`) + "$"
	ok, err := regexp.MatchString(re, name)
	return err == nil && ok
}

type fileMeta struct {
	Prot     int    `json:"prot"`
	Modified string `json:"modified"`
	Cluster  int    `json:"cluster,omitempty"`
	Alloc    int    `json:"alloc,omitempty"`
}

type recLock struct {
	job int
}

type Disk struct {
	mu       sync.Mutex
	Root     string
	SY       string
	packs    []*Pack
	locks    map[string]recLock
	quotaOf  func(proj, prog int) int
	fileJobs map[string]map[int]int
	fileExcl map[string]int
}

// setAside renames a control file the loader could not parse, so a fresh
// default can be written in its place while the damaged one is kept for
// inspection. A garbled accounts.json or packs.json should not stop the
// system from booting.
func setAside(path string, cause error) {
	backup := path + ".bad"
	if err := os.Rename(path, backup); err != nil {
		return
	}
	fmt.Fprintf(os.Stderr, "?%s is damaged (%v)\n", filepath.Base(path), cause)
	fmt.Fprintf(os.Stderr, "  saved as %s, rebuilding defaults\n", filepath.Base(backup))
}

func OpenDisk(root string) (*Disk, error) {
	sy := filepath.Join(root, "SY")
	if err := os.MkdirAll(sy, 0o755); err != nil {
		return nil, err
	}
	d := &Disk{Root: root, SY: sy, locks: map[string]recLock{}, fileJobs: map[string]map[int]int{}, fileExcl: map[string]int{}}
	if err := d.loadPacks(); err != nil {
		return nil, err
	}
	if err := d.ensureUnitDirs(); err != nil {
		return nil, err
	}
	if err := d.seedPayrolPack(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Disk) seedPayrolPack() error {
	d.mu.Lock()
	need := false
	for _, p := range d.packs {
		if p != nil && p.Dev == "DB" && p.Unit == 1 && !p.Init {
			need = true
			break
		}
	}
	d.mu.Unlock()
	if !need {
		return nil
	}
	if err := d.Initialize("DB", 1, "PAYROL", false, true); err != nil {
		return err
	}
	dir := filepath.Join(d.Root, "DB1", "100,100")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	note := filepath.Join(dir, "README.TXT")
	if _, err := os.Stat(note); err == nil {
		return nil
	}
	return os.WriteFile(note, []byte("Private pack PAYROL on DB1:.\nMOUNT DB1: PAYROL  then  DIR DB1:\n"), 0o644)
}

func (d *Disk) AccountDir(proj, prog int) (string, error) {
	return d.accountDir(d.systemPack(), proj, prog)
}

func (d *Disk) RemoveAccount(proj, prog int) error {
	if proj == 1 && prog == 2 {
		return fsErr("Protection violation")
	}
	path := filepath.Join(d.SY, fmt.Sprintf("%d,%d", proj, prog))
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return nil
}

func (d *Disk) indexPath(folder string) string {
	return filepath.Join(folder, ".rsts-index.json")
}

func (d *Disk) loadIndex(folder string) map[string]fileMeta {
	out := map[string]fileMeta{}
	data, err := os.ReadFile(d.indexPath(folder))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func (d *Disk) saveIndex(folder string, index map[string]fileMeta) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(d.indexPath(folder), append(data, '\n'), 0o644)
}

func (d *Disk) touchMeta(folder, filename string, prot int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	index := d.loadIndex(folder)
	meta := index[filename]
	if prot != 0 {
		meta.Prot = prot
	}
	meta.Modified = time.Now().Format("2006-01-02T15:04:05")
	index[filename] = meta
	return d.saveIndex(folder, index)
}

func (d *Disk) SetFileAlloc(path string, cluster, alloc int) error {
	if path == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	folder := filepath.Dir(path)
	base := filepath.Base(path)
	index := d.loadIndex(folder)
	meta := index[base]
	if meta.Prot == 0 {
		meta.Prot = defaultProt
	}
	if cluster > 0 {
		meta.Cluster = cluster
	}
	if alloc > 0 {
		meta.Alloc = alloc
	}
	meta.Modified = time.Now().Format("2006-01-02T15:04:05")
	index[base] = meta
	return d.saveIndex(folder, index)
}

func lockKey(path string, rec int) string {
	return strings.ToLower(path) + "#" + strconv.Itoa(rec)
}

func (d *Disk) lockRecord(path string, rec, job int) error {
	if d == nil || path == "" || rec < 1 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.locks == nil {
		d.locks = map[string]recLock{}
	}
	key := lockKey(path, rec)
	if held, ok := d.locks[key]; ok && held.job != job {
		return fsErr("Disk block is interlocked")
	}
	d.locks[key] = recLock{job: job}
	return nil
}

func (d *Disk) claimFile(path string, job int, exclusive bool) error {
	if d == nil || path == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	key := strings.ToLower(path)
	if d.fileExcl == nil {
		d.fileExcl = map[string]int{}
	}
	if d.fileJobs == nil {
		d.fileJobs = map[string]map[int]int{}
	}
	if held, ok := d.fileExcl[key]; ok && held != job {
		return fsErr("Account or device in use")
	}
	if exclusive {
		for j, n := range d.fileJobs[key] {
			if j != job && n > 0 {
				return fsErr("Account or device in use")
			}
		}
		d.fileExcl[key] = job
	}
	if d.fileJobs[key] == nil {
		d.fileJobs[key] = map[int]int{}
	}
	d.fileJobs[key][job]++
	return nil
}

func (d *Disk) releaseFile(path string, job int) {
	if d == nil || path == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	key := strings.ToLower(path)
	if jobs := d.fileJobs[key]; jobs != nil {
		jobs[job]--
		if jobs[job] <= 0 {
			delete(jobs, job)
		}
		if len(jobs) == 0 {
			delete(d.fileJobs, key)
		}
	}
	if d.fileExcl[key] == job {
		delete(d.fileExcl, key)
	}
}

func (d *Disk) unlockRecord(path string, rec, job int) {
	if d == nil || path == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	key := lockKey(path, rec)
	if held, ok := d.locks[key]; ok && (job == 0 || held.job == job) {
		delete(d.locks, key)
	}
}

func (d *Disk) ResolveFolder(spec FileSpec, curProj, curProg int) (string, error) {
	proj, prog := curProj, curProg
	if spec.Proj != nil {
		proj, prog = *spec.Proj, *spec.Prog
	}
	return d.AccountDir(proj, prog)
}

func (d *Disk) ListDir(spec FileSpec, curProj, curProg int, privileged bool) (string, []FileInfo, error) {
	pack, err := d.resolvePack(spec)
	if err != nil {
		return "", nil, err
	}
	proj, prog := curProj, curProg
	if spec.Proj != nil {
		proj, prog = *spec.Proj, *spec.Prog
	}
	if !privileged && (proj != curProj || prog != curProg) {
		return "", nil, fsErr("Protection violation")
	}
	folder, err := d.accountDir(pack, proj, prog)
	if err != nil {
		return "", nil, err
	}
	index := d.loadIndex(folder)
	entries, err := os.ReadDir(folder)
	if err != nil {
		return "", nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var infos []FileInfo
	for _, ent := range entries {
		if ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		filename := strings.ToUpper(ent.Name())
		name, ext := filename, ""
		if i := strings.IndexByte(filename, '.'); i >= 0 {
			name, ext = filename[:i], filename[i+1:]
		}
		if spec.Name != "" && spec.Name != "*" && !wildMatch(name, spec.Name) {
			continue
		}
		if spec.Ext != "" && spec.Ext != "*" && !wildMatch(ext, spec.Ext) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		meta, ok := index[ent.Name()]
		if !ok {
			meta = index[filename]
		}
		mod := info.ModTime()
		if meta.Modified != "" {
			if t, err := time.Parse("2006-01-02T15:04:05", meta.Modified); err == nil {
				mod = t
			}
		}
		prot := defaultProt
		if meta.Prot != 0 {
			prot = meta.Prot
		}
		infos = append(infos, FileInfo{
			Name:     filename,
			Size:     info.Size(),
			Prot:     prot,
			Modified: mod,
			Cluster:  meta.Cluster,
			Alloc:    meta.Alloc,
			Path:     filepath.Join(folder, ent.Name()),
		})
	}
	return fmt.Sprintf("%d,%d", proj, prog), infos, nil
}

func (d *Disk) protOf(path string) int {
	folder := filepath.Dir(path)
	base := filepath.Base(path)
	index := d.loadIndex(folder)
	meta, ok := index[base]
	if !ok {
		meta = index[strings.ToUpper(base)]
	}
	if meta.Prot != 0 {
		return meta.Prot
	}
	return defaultProt
}

func (d *Disk) Prot(spec FileSpec, curProj, curProg int, privileged bool) (int, error) {
	path, _, _, err := d.locate(spec, curProj, curProg, privileged, true)
	if err != nil {
		return 0, err
	}
	return d.protOf(path), nil
}

func (d *Disk) checkAccess(path string, fileProj, fileProg, curProj, curProg int, privileged bool, op accessOp) error {
	owner := fileProj == curProj && fileProg == curProg
	group := fileProj == curProj
	if !protAllows(d.protOf(path), owner, group, privileged, op) {
		return fsErr("Protection violation")
	}
	return nil
}

func (d *Disk) ReadText(spec FileSpec, curProj, curProg int, privileged bool) (string, error) {
	text, _, err := d.readOp(spec, curProj, curProg, privileged, accRead)
	return text, err
}

func (d *Disk) ReadExecute(spec FileSpec, curProj, curProg int, privileged bool) (string, int, error) {
	return d.readOp(spec, curProj, curProg, privileged, accExecute)
}

func (d *Disk) readOp(spec FileSpec, curProj, curProg int, privileged bool, op accessOp) (string, int, error) {
	path, proj, prog, err := d.locate(spec, curProj, curProg, privileged, true)
	if err != nil {
		return "", 0, err
	}
	if err := d.checkAccess(path, proj, prog, curProj, curProg, privileged, op); err != nil {
		return "", 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return string(data), d.protOf(path), nil
}

func (d *Disk) WriteText(spec FileSpec, curProj, curProg int, privileged bool, content string, prot int) error {
	if spec.ProtSet {
		prot = spec.Prot
	}
	if prot == 0 {
		prot = defaultProt
	}
	if err := checkPrivProt(prot, privileged); err != nil {
		return err
	}
	if err := d.rejectWrite(spec); err != nil {
		return err
	}
	path, proj, prog, err := d.locate(spec, curProj, curProg, privileged, false)
	if err != nil {
		return err
	}
	owner := proj == curProj && prog == curProg
	if !privileged && !owner {
		return fsErr("Protection violation")
	}
	if _, err := os.Stat(path); err == nil {
		if err := d.checkAccess(path, proj, prog, curProj, curProg, privileged, accWrite); err != nil {
			return err
		}
	}
	if err := d.enforceQuota(filepath.Dir(path), path, proj, prog, int64(len(content))); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	return d.touchMeta(filepath.Dir(path), filepath.Base(path), prot)
}

func (d *Disk) SetProt(spec FileSpec, curProj, curProg int, privileged bool, prot int) error {
	if err := checkPrivProt(prot, privileged); err != nil {
		return err
	}
	path, proj, prog, err := d.locate(spec, curProj, curProg, privileged, true)
	if err != nil {
		return err
	}
	if err := d.checkAccess(path, proj, prog, curProj, curProg, privileged, accWrite); err != nil {
		return err
	}
	return d.touchMeta(filepath.Dir(path), filepath.Base(path), prot)
}

func (d *Disk) Delete(spec FileSpec, curProj, curProg int, privileged bool) error {
	path, proj, prog, err := d.locate(spec, curProj, curProg, privileged, true)
	if err != nil {
		return err
	}
	if err := d.checkAccess(path, proj, prog, curProj, curProg, privileged, accDelete); err != nil {
		return err
	}
	folder := filepath.Dir(path)
	if err := os.Remove(path); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	index := d.loadIndex(folder)
	delete(index, filepath.Base(path))
	return d.saveIndex(folder, index)
}

func sameSpecFile(a, b FileSpec, curProj, curProg int) bool {
	ap, aq := curProj, curProg
	if a.Proj != nil {
		ap, aq = *a.Proj, *a.Prog
	}
	bp, bq := curProj, curProg
	if b.Proj != nil {
		bp, bq = *b.Proj, *b.Prog
	}
	return ap == bp && aq == bq && strings.EqualFold(a.Filename(), b.Filename())
}

func (d *Disk) Rename(old, new FileSpec, curProj, curProg int, privileged bool) error {
	if new.ProtSet {
		if err := checkPrivProt(new.Prot, privileged); err != nil {
			return err
		}
	}
	if new.ProtSet && sameSpecFile(old, new, curProj, curProg) {
		return d.SetProt(old, curProj, curProg, privileged, new.Prot)
	}
	src, proj, prog, err := d.locate(old, curProj, curProg, privileged, true)
	if err != nil {
		return err
	}
	if err := d.checkAccess(src, proj, prog, curProj, curProg, privileged, accWrite); err != nil {
		return err
	}
	dst, dproj, dprog, err := d.locate(new, curProj, curProg, privileged, false)
	if err != nil {
		return err
	}
	downer := dproj == curProj && dprog == curProg
	if !privileged && !downer {
		return fsErr("Protection violation")
	}
	if _, err := os.Stat(dst); err == nil {
		if sameSpecFile(old, new, curProj, curProg) {
			if new.ProtSet {
				return d.SetProt(old, curProj, curProg, privileged, new.Prot)
			}
			return nil
		}
		return fsErr("File exists")
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	index := d.loadIndex(filepath.Dir(src))
	meta := index[filepath.Base(src)]
	delete(index, filepath.Base(src))
	if meta.Prot == 0 {
		meta.Prot = defaultProt
	}
	if new.ProtSet {
		meta.Prot = new.Prot
	}
	meta.Modified = time.Now().Format("2006-01-02T15:04:05")
	index[filepath.Base(dst)] = meta
	return d.saveIndex(filepath.Dir(dst), index)
}

func (d *Disk) Copy(src, dst FileSpec, curProj, curProg int, privileged bool) error {
	return d.copyFile(src, dst, curProj, curProg, privileged, false, false)
}

// copyFile is PIP's copy: /AP concatenates onto an existing dest, /NE
// fails if dest exists (error 16), /PROT:n overrides the source protection.
func (d *Disk) copyFile(src, dst FileSpec, curProj, curProg int, privileged bool, appendTo, noSuper bool) error {
	if noSuper && d.Exists(dst, curProj, curProg, privileged) {
		return fsErr("Name or account now exists")
	}
	text, srcProt, err := d.readOp(src, curProj, curProg, privileged, accRead)
	if err != nil {
		return err
	}
	if appendTo && d.Exists(dst, curProj, curProg, privileged) {
		old, _, err := d.readOp(dst, curProj, curProg, privileged, accRead)
		if err != nil {
			return err
		}
		text = old + text
	}
	prot := srcProt
	if dst.ProtSet {
		prot = dst.Prot
	}
	return d.WriteText(dst, curProj, curProg, privileged, text, prot)
}

func (d *Disk) Exists(spec FileSpec, curProj, curProg int, privileged bool) bool {
	_, _, _, err := d.locate(spec, curProj, curProg, privileged, true)
	return err == nil
}

func (d *Disk) filePath(spec FileSpec, curProj, curProg int, privileged, mustExist bool) (string, error) {
	path, _, _, err := d.locate(spec, curProj, curProg, privileged, mustExist)
	return path, err
}

func (d *Disk) locate(spec FileSpec, curProj, curProg int, privileged, mustExist bool) (string, int, int, error) {
	if spec.Wildcard {
		return "", 0, 0, fsErr("Illegal wild card")
	}
	pack, err := d.resolvePack(spec)
	if err != nil {
		return "", 0, 0, err
	}
	proj, prog := curProj, curProg
	if spec.Proj != nil {
		proj, prog = *spec.Proj, *spec.Prog
	}
	if !privileged && (proj != curProj || prog != curProg) && !(proj == 1 && prog == 2) {
		return "", 0, 0, fsErr("Protection violation")
	}
	if spec.Name == "*" || spec.Name == "" {
		return "", 0, 0, fsErr("Illegal file name")
	}
	filename := spec.Filename()
	folder, err := d.accountDir(pack, proj, prog)
	if err != nil {
		return "", 0, 0, err
	}
	path := filepath.Join(folder, filename)
	if _, err := os.Stat(path); err != nil {
		entries, _ := os.ReadDir(folder)
		found := false
		for _, ent := range entries {
			if !ent.IsDir() && strings.EqualFold(ent.Name(), filename) {
				path = filepath.Join(folder, ent.Name())
				found = true
				break
			}
		}
		if mustExist && !found {
			return "", 0, 0, fsErr("Can't find file or account")
		}
	}
	if mustExist {
		if _, err := os.Stat(path); err != nil {
			return "", 0, 0, fsErr("Can't find file or account")
		}
	}
	return path, proj, prog, nil
}
