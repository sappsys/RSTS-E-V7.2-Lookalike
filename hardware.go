package rsts

import (
	"encoding/binary"
	"math"
	"time"
)

// PDP-11/70 figures as RSTS/E V7.2 saw them (22-bit physical space).
const (
	CPUName      = "PDP-11/70"
	MemoryKW     = 1920 // K-words of RAM; 3840 KB. Top 4 KW is the I/O page.
	MemoryMaxKW  = 2048 // 22-bit byte space = 4 MB = 2048 KW
	CacheKB      = 2    // bipolar cache on the 11/70
	MaxJobs      = 63
	ClockHz      = 60 // KW11-L line-frequency clock (60 Hz US / 50 Hz elsewhere)
	FPPName      = "FP11-C"
	MassbusName  = "RH70"
	UnibusName   = "UNIBUS"
	SystemDisk   = "RP06"
	ConsoleName  = "DL11"
	SwitchReg    = 0o177570
	KW11CSR      = 0o177546
	PSWAddr      = 0o177776
	cfgPtrAddr   = 156
	cfgBase      = 400
	diskPtrAddr  = 2000
	jobPtrAddr   = 2200
	cachePtrAddr = 2400
	jobtblAddr   = 1000
	jbstatAddr   = 1200
	jbwaitAddr   = 1400
	dateAddr     = 512
	minAddr      = 514
	secAddr      = 516
	jobnoAddr    = 518
)

func rstsDateInt(t time.Time) int {
	return (t.Year()-1970)*1000 + t.YearDay()
}

func minutesToMidnight(t time.Time) int {
	left := 24*60 - (t.Hour()*60 + t.Minute())
	if left < 0 {
		return 0
	}
	return left
}

func putLE(buf []byte, oneBased int, word int) {
	i := oneBased - 1
	if i < 0 || i+1 >= len(buf) {
		return
	}
	binary.LittleEndian.PutUint16(buf[i:i+2], uint16(word))
}

// UU.TB1 — SYS(CHR$(6%)+CHR$(-3%)), 30 bytes.
// After CHANGE TO T%, T%(4%) is max jobs; T%(11%)+SWAP%(T%(12%)) is JOBTBL.
func sysTableTB1() string {
	buf := make([]byte, 30)
	buf[3] = MaxJobs // 1-based index 4
	putLE(buf, 5, MemoryKW)
	putLE(buf, 7, ClockHz)
	putLE(buf, 11, jobtblAddr)
	putLE(buf, 13, jbstatAddr)
	putLE(buf, 15, jbwaitAddr)
	putLE(buf, 17, cfgBase)
	putLE(buf, 19, CacheKB)
	return string(buf)
}

// UU.TB2 — SYS(CHR$(6%)+CHR$(-12%))
func sysTableTB2() string {
	buf := make([]byte, 30)
	putLE(buf, 1, ClockHz)
	putLE(buf, 3, MemoryKW)
	putLE(buf, 5, MaxJobs)
	putLE(buf, 7, cachePtrAddr)
	putLE(buf, 9, diskPtrAddr)
	return string(buf)
}

func sysIdentString() string {
	// STATS.BAS does CVT$$(RIGHT$(SYS(CHR$(6%)+CHR$(9%)+CHR$(0%)),3%),4%)
	return "  " + SystemName
}

func peekWord(addr int, job int) int {
	a := addr & 0xFFFE
	if job < 1 {
		job = 1
	}
	now := time.Now()
	switch a {
	case cfgPtrAddr:
		return cfgBase
	case cfgBase:
		return diskPtrAddr
	case cfgBase + 2:
		return jobPtrAddr
	case cfgBase + 6:
		return cachePtrAddr
	case diskPtrAddr:
		return 1 // one disk, SY:
	case jobPtrAddr:
		return ClockHz
	case cachePtrAddr:
		return CacheKB
	case dateAddr:
		return rstsDateInt(now)
	case minAddr:
		return minutesToMidnight(now)
	case secAddr:
		sec := 60 - now.Second()
		if sec == 60 {
			sec = 0
		}
		ticks := (now.Nanosecond() / 1e6) * ClockHz / 1000
		return (ticks << 8) | (sec & 255)
	case jobnoAddr:
		return (job * 2) & 255
	case jobtblAddr:
		return job
	case jbstatAddr, jbwaitAddr:
		return 0
	case SwitchReg:
		return 0
	case KW11CSR:
		return 0
	case PSWAddr:
		return 0
	case 36:
		return rstsDateInt(now)
	case 38:
		return minutesToMidnight(now)
	default:
		return 0
	}
}

func swapPercent(n float64) float64 {
	v := uint16(int16(int(math.Round(n))))
	sw := (v << 8) | (v >> 8)
	return float64(int16(sw))
}

func secondsSinceMidnight() float64 {
	now := time.Now()
	return float64(now.Hour()*3600+now.Minute()*60+now.Second()) + float64(now.Nanosecond())/1e9
}
