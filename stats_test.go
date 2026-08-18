package rsts

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type quietTerm struct{ in []string }

func (t *quietTerm) ReadLine(string) (string, error) {
	if len(t.in) == 0 {
		return "", os.ErrClosed
	}
	s := t.in[0]
	t.in = t.in[1:]
	return s, nil
}

func (t *quietTerm) ReadPassword(p string) (string, error) { return t.ReadLine(p) }

func TestJobSizeTracksStorage(t *testing.T) {
	m := NewMachine(IO{})
	empty := m.SizeKW()
	if empty != minJobKW {
		t.Fatalf("bare job is %dK, want %dK", empty, minJobKW)
	}

	if err := m.LoadSource("10 DIM A(20000)\n20 A(20000)=1\n30 END\n", "BIG"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	big := m.SizeKW()

	// 20001 elements of two words each, in K-words, give or take the
	// program text and the job's own overhead.
	want := 20001 * floatBytes / kWordBytes
	if big < want {
		t.Fatalf("array job is %dK, want at least %dK", big, want)
	}
	if big > want+8 {
		t.Fatalf("array job is %dK, which is more than the array explains (%dK)", big, want)
	}
}

func TestStringStorageCounted(t *testing.T) {
	m := NewMachine(IO{})
	before := m.MemoryBytes()
	if err := m.LoadSource("10 A$=STRING$(4000,\"X\")\n20 END\n", "STR"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if grew := m.MemoryBytes() - before; grew < 4000 {
		t.Fatalf("a 4000 character string grew the job by %d bytes", grew)
	}
}

// A virtual array lives in its file, so it must not be charged to the job.
func TestVirtualArrayNotCountedAsMemory(t *testing.T) {
	dir := t.TempDir()
	m := NewMachine(IO{
		Open: func(m *Machine, ch int, path, mode string) error {
			f, err := os.OpenFile(filepath.Join(dir, path), os.O_RDWR|os.O_CREATE, 0o644)
			if err != nil {
				return err
			}
			m.Files[ch] = &chanFile{file: f, mode: mode}
			return nil
		},
	})
	src := `10 OPEN "BIG.VIR" AS FILE 1
20 DIM #1, V(20000)
30 V(20000)=1
40 END
`
	if err := m.LoadSource(src, "VIRT"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if kw := m.SizeKW(); kw > 8 {
		t.Fatalf("virtual array charged to the job: %dK", kw)
	}
}

func TestCPUTimeExcludesSleep(t *testing.T) {
	m := NewMachine(IO{})
	if err := m.LoadSource("10 SLEEP 1\n20 END\n", "NAP"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("SLEEP 1 only took %v", elapsed)
	}
	if cpu := m.CPUTime(); cpu > 500*time.Millisecond {
		t.Fatalf("sleeping was charged as CPU: %v", cpu)
	}
}

func TestCPUTimeExcludesInputWait(t *testing.T) {
	c := &capture{inputs: []string{"7"}}
	m := NewMachine(IO{
		Write: c.write,
		Read: func(prompt string) (string, error) {
			time.Sleep(300 * time.Millisecond)
			return c.read(prompt)
		},
	})
	if err := m.LoadSource("10 INPUT A\n20 END\n", "ASK"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if cpu := m.CPUTime(); cpu > 200*time.Millisecond {
		t.Fatalf("waiting at INPUT was charged as CPU: %v", cpu)
	}
}

func TestCPUTimeCountsWork(t *testing.T) {
	m := NewMachine(IO{})
	if err := m.LoadSource("10 FOR I=1 TO 200000\n20 X=X+I\n30 NEXT I\n40 END\n", "WORK"); err != nil {
		t.Fatal(err)
	}
	if err := m.RunProgram(); err != nil {
		t.Fatal(err)
	}
	if m.CPUTime() <= 0 {
		t.Fatal("a compute loop was charged no CPU at all")
	}
}

func TestMemoryReportAddsUpAcrossJobs(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), Config{MaxUsers: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer sys.Close()

	var shells []*Shell
	for i := 0; i < 3; i++ {
		job, err := sys.Attach("TEST")
		if err != nil {
			t.Fatal(err)
		}
		sh := sys.newSession(job, &bytes.Buffer{}, &quietTerm{})
		sh.Login("GUEST", "GUEST")
		shells = append(shells, sh)
	}
	// Give one job a program so the jobs differ in size.
	if err := shells[0].Basic.LoadSource("10 DIM A(5000)\n20 A(5000)=1\n30 END\n", "BIG"); err != nil {
		t.Fatal(err)
	}
	if err := shells[0].Basic.RunProgram(); err != nil {
		t.Fatal(err)
	}
	shells[0].syncJob()

	want := 0
	for _, sh := range shells {
		want += sh.Basic.SizeKW()
	}
	if want <= 3*minJobKW {
		t.Fatalf("expected the loaded job to raise the total, got %dK", want)
	}

	var out bytes.Buffer
	shells[0].out = &out
	shells[0].printSystatMemory()
	text := out.String()

	if !strings.Contains(text, fmt.Sprintf("User      %5dK  3 jobs", want)) {
		t.Fatalf("user total is not the sum of the jobs (%dK):\n%s", want, text)
	}
	if !strings.Contains(text, "shared by 3 jobs") {
		t.Fatalf("the RTS should be shared, not counted per job:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("Free      %5dK", MemoryKW-MonitorKW-RTSKW-want)) {
		t.Fatalf("free memory does not balance:\n%s", text)
	}
}

func TestDiskUsageIsMeasured(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sys.Close()
	pack := sys.Disk.systemPack()

	capacity, before := sys.Disk.PackUsage(pack)
	if capacity != packCapacity("RP06") {
		t.Fatalf("capacity %d", capacity)
	}
	if before <= 0 || before >= capacity {
		t.Fatalf("used %d blocks of %d", before, capacity)
	}

	spec, err := ParseFileSpec("[100,100]FAT.DAT", "")
	if err != nil {
		t.Fatal(err)
	}
	// 20 blocks of data, which the RP06 cluster size rounds to 20.
	body := strings.Repeat("X", 20*blockSize)
	if err := sys.Disk.WriteText(spec, 100, 100, true, body, defaultProt); err != nil {
		t.Fatal(err)
	}

	_, after := sys.Disk.PackUsage(pack)
	if grew := after - before; grew != 20 {
		t.Fatalf("a 20 block file changed usage by %d blocks", grew)
	}
}

func TestOpenFileCountIsReal(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sys.Close()
	job, err := sys.Attach("TEST")
	if err != nil {
		t.Fatal(err)
	}
	sh := sys.newSession(job, &bytes.Buffer{}, &quietTerm{})
	sh.Login("GUEST", "GUEST")
	pack := sys.Disk.systemPack()

	if n := sys.openOnPack(pack); n != 0 {
		t.Fatalf("%d files open before we started", n)
	}
	if err := sh.Basic.ExecImmediate(`OPEN "COUNT.TMP" FOR OUTPUT AS FILE 1`); err != nil {
		t.Fatal(err)
	}
	if n := sys.openOnPack(pack); n != 1 {
		t.Fatalf("open count is %d, want 1", n)
	}
	if err := sh.Basic.ExecImmediate("CLOSE 1"); err != nil {
		t.Fatal(err)
	}
	if n := sys.openOnPack(pack); n != 0 {
		t.Fatalf("open count is %d after CLOSE, want 0", n)
	}
}

// Reusing a channel must release the previous file, not leak it.
func TestReopeningChannelReleasesFile(t *testing.T) {
	sys, err := NewSystem(t.TempDir(), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sys.Close()
	job, err := sys.Attach("TEST")
	if err != nil {
		t.Fatal(err)
	}
	sh := sys.newSession(job, &bytes.Buffer{}, &quietTerm{})
	sh.Login("GUEST", "GUEST")

	for i := 0; i < 5; i++ {
		if err := sh.Basic.ExecImmediate(`OPEN "REUSE.TMP" FOR OUTPUT AS FILE 1`); err != nil {
			t.Fatal(err)
		}
	}
	if n := sys.openOnPack(sys.Disk.systemPack()); n != 1 {
		t.Fatalf("open count is %d after five opens on one channel, want 1", n)
	}
	sh.Basic.CloseAllFiles()
	if n := sys.openOnPack(sys.Disk.systemPack()); n != 0 {
		t.Fatalf("open count is %d after closing, want 0", n)
	}
}
