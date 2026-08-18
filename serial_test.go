package rsts

import (
	"reflect"
	"strings"
	"testing"
)

func TestConfigSerialList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`serial = "/dev/ttyUSB0"`, []string{"/dev/ttyUSB0"}},
		{`serial = "/dev/ttyUSB0,/dev/ttyS0"`, []string{"/dev/ttyUSB0", "/dev/ttyS0"}},
		{`serial = "/dev/ttyUSB0, /dev/ttyS0 , /dev/ttyS1"`,
			[]string{"/dev/ttyUSB0", "/dev/ttyS0", "/dev/ttyS1"}},
		{`serial = ""`, nil},
		{`serial = " , "`, nil},
		{`serial = "/dev/ttyS0"  # one line`, []string{"/dev/ttyS0"}},
	}
	for _, c := range cases {
		cfg := DefaultConfig()
		if err := parseTOML(c.in, &cfg); err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		cfg.clamp()
		if !reflect.DeepEqual(cfg.Serial, c.want) {
			t.Errorf("%s gave %#v, want %#v", c.in, cfg.Serial, c.want)
		}
	}
}

func TestConfigSerialDefaultsToNone(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.Serial) != 0 {
		t.Fatalf("default config has serial lines: %#v", cfg.Serial)
	}
	// The file written on first run must parse back to the same thing.
	if err := parseTOML(defaultConfigTOML, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Serial) != 0 {
		t.Fatalf("the default config.toml turns on serial lines: %#v", cfg.Serial)
	}
	if !strings.Contains(defaultConfigTOML, "serial") {
		t.Fatal("the default config.toml should mention serial")
	}
}
