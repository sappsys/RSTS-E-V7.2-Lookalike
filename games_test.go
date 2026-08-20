package rsts

import (
	"strings"
	"testing"
)

var guestGames = []string{
	"GUESS.BAS", "NIM.BAS", "HANGMN.BAS", "TICTAC.BAS",
	"LANDER.BAS", "WUMPUS.BAS", "BLACKJ.BAS", "SLOTS.BAS", "ACEY.BAS",
}

func TestGuestGamesCompile(t *testing.T) {
	for _, name := range guestGames {
		src := samples["100,100"][name]
		if src == "" {
			t.Fatalf("missing sample %s", name)
		}
		if err := NewMachine(IO{}).LoadSource(src, strings.TrimSuffix(name, ".BAS")); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestGuestGamesQuit(t *testing.T) {
	cases := []struct {
		name   string
		inputs []string
		want   string
	}{
		{"NIM.BAS", []string{"0"}, "ENOUGH"},
		{"HANGMN.BAS", []string{"*"}, "ENOUGH"},
		{"TICTAC.BAS", []string{"0"}, "ENOUGH"},
		{"LANDER.BAS", []string{"-1"}, "ENOUGH"},
		{"WUMPUS.BAS", []string{"M", "0"}, "ENOUGH"},
		{"BLACKJ.BAS", []string{"0"}, "YOU LEAVE WITH"},
		{"SLOTS.BAS", []string{"0"}, "YOU WALK AWAY WITH"},
		{"ACEY.BAS", []string{"0"}, "YOU LEAVE WITH"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runProgram(t, samples["100,100"][c.name], c.inputs...)
			if !strings.Contains(out, c.want) {
				t.Fatalf("output %q, want %q", out, c.want)
			}
		})
	}
}

func TestNimPlayerWins(t *testing.T) {
	// 21 matches, last wins. Take 1 leaving 20, then always answer 4 minus
	// whatever the computer took.
	out := runProgram(t, samples["100,100"]["NIM.BAS"],
		"1", "3", "3", "3", "3", "3", "N")
	if !strings.Contains(out, "YOU WIN") {
		t.Fatalf("expected a player win:\n%s", out)
	}
}

func TestGuestGamesSeededOnDisk(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	for _, name := range guestGames {
		spec, err := ParseFileSpec(name, "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := sh.Disk.ReadText(spec, 100, 100, false)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != samples["100,100"][name] {
			t.Errorf("%s on disk does not match the sample", name)
		}
	}
}
