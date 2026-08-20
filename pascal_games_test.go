package rsts

import (
	"strings"
	"testing"
)

var guestPascalGames = []string{
	"BAGELS.PAS", "HAMURA.PAS", "HUNT.PAS", "MAZE.PAS", "CHOMP.PAS", "CRAPS.PAS",
}

func TestPascalGamesCompile(t *testing.T) {
	for _, name := range guestPascalGames {
		src := samples["100,100"][name]
		if src == "" {
			t.Fatalf("missing sample %s", name)
		}
		if _, err := CompilePascal(src); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPascalGamesQuit(t *testing.T) {
	cases := []struct {
		name   string
		inputs []string
		want   string
	}{
		{"BAGELS.PAS", []string{"1", "0"}, "THE NUMBER WAS"},
		{"HAMURA.PAS", []string{"1", "-1"}, "YOU LEAVE"},
		{"HUNT.PAS", []string{"1", "Q"}, "ENOUGH"},
		{"MAZE.PAS", []string{"Q"}, "ENOUGH"},
		{"CHOMP.PAS", []string{"0 0"}, "ENOUGH"},
		{"CRAPS.PAS", []string{"1", "0"}, "YOU LEAVE WITH"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runPascal(t, samples["100,100"][c.name], c.inputs...)
			if !strings.Contains(out, c.want) {
				t.Fatalf("output %q, want %q", out, c.want)
			}
		})
	}
}

func TestPascalMazeEscape(t *testing.T) {
	out := runPascal(t, samples["100,100"]["MAZE.PAS"],
		"E", "E", "E", "E", "S", "S", "S")
	if !strings.Contains(out, "YOU ESCAPED") {
		t.Fatalf("expected an escape:\n%s", out)
	}
}

func TestPascalChompPoison(t *testing.T) {
	out := runPascal(t, samples["100,100"]["CHOMP.PAS"], "1 1")
	if !strings.Contains(out, "YOU ATE THE POISON") {
		t.Fatalf("expected poison:\n%s", out)
	}
}

func TestPascalGamesSeededOnDisk(t *testing.T) {
	sh, err := NewShell(t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	sh.Login("GUEST", "GUEST")
	for _, name := range guestPascalGames {
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
