package rsts

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultConfigName = "config.toml"

const defaultConfigTOML = "# " + SystemLong + `  (PDP-11/70)
#
# telnet_port 23 needs root or CAP_NET_BIND_SERVICE on Unix.
# Use 2323 (or any port >1023) if you are not root:
#   telnet_port = 2323

# Serial lines, comma separated. Each one runs at 9600 8N1 and behaves
# exactly like a Telnet line: one job per line, its own KB:, and the
# line is offered again when the user logs off.
#   serial = "/dev/ttyUSB0,/dev/ttyS0"

max_users = 25
telnet_port = 23
telnet_bind = "0.0.0.0"
telnet = true
console = true
serial = ""
`

// Config is loaded from config.toml. MaxUsers is this emulator's
// simultaneous-job cap (real RSTS/E on an 11/70 allowed 63).
type Config struct {
	MaxUsers   int
	TelnetPort int
	TelnetBind string
	Telnet     bool
	Console    bool
	// Serial lines to answer, each at 9600 8N1.
	Serial []string
}

func DefaultConfig() Config {
	return Config{
		MaxUsers:   25,
		TelnetPort: 23,
		TelnetBind: "0.0.0.0",
		Telnet:     true,
		Console:    true,
	}
}

func (c *Config) clamp() {
	if c.MaxUsers < 1 {
		c.MaxUsers = 1
	}
	if c.MaxUsers > MaxJobs {
		c.MaxUsers = MaxJobs
	}
	if c.TelnetPort < 0 {
		c.TelnetPort = 0
		c.Telnet = false
	}
	if c.TelnetPort > 65535 {
		c.TelnetPort = 23
	}
	if strings.TrimSpace(c.TelnetBind) == "" {
		c.TelnetBind = "0.0.0.0"
	}
	c.Serial = splitSerial(strings.Join(c.Serial, ","))
}

// splitSerial reads the comma separated device list, ignoring blanks so
// that serial = "" means no lines at all.
func splitSerial(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if dev := strings.TrimSpace(part); dev != "" {
			out = append(out, dev)
		}
	}
	return out
}

func LoadConfig(path string) (Config, string, error) {
	cfg := DefaultConfig()
	if path == "" {
		path = defaultConfigName
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(defaultConfigTOML), 0o644); err != nil {
				return cfg, path, fmt.Errorf("create %s: %w", path, err)
			}
			cfg.clamp()
			return cfg, path, nil
		}
		return cfg, path, err
	}
	if err := parseTOML(string(data), &cfg); err != nil {
		return cfg, path, fmt.Errorf("%s: %w", path, err)
	}
	cfg.clamp()
	return cfg, path, nil
}

func parseTOML(src string, cfg *Config) error {
	for n, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("line %d: expected key = value", n+1)
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(val)
		if i := unquotedHash(val); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		switch key {
		case "max_users":
			n, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("max_users: %w", err)
			}
			cfg.MaxUsers = n
		case "telnet_port":
			n, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("telnet_port: %w", err)
			}
			cfg.TelnetPort = n
		case "telnet_bind":
			s, err := tomlString(val)
			if err != nil {
				return err
			}
			cfg.TelnetBind = s
		case "telnet":
			b, err := tomlBool(val)
			if err != nil {
				return fmt.Errorf("telnet: %w", err)
			}
			cfg.Telnet = b
		case "console":
			b, err := tomlBool(val)
			if err != nil {
				return fmt.Errorf("console: %w", err)
			}
			cfg.Console = b
		case "serial":
			s, err := tomlString(val)
			if err != nil {
				return fmt.Errorf("serial: %w", err)
			}
			cfg.Serial = splitSerial(s)
		}
	}
	return nil
}

func unquotedHash(val string) int {
	inq := false
	esc := false
	for i, r := range val {
		if inq {
			if esc {
				esc = false
				continue
			}
			if r == '\\' {
				esc = true
				continue
			}
			if r == '"' {
				inq = false
			}
			continue
		}
		if r == '"' {
			inq = true
			continue
		}
		if r == '#' {
			return i
		}
	}
	return -1
}

func tomlString(val string) (string, error) {
	if strings.HasPrefix(val, "\"") {
		s, err := strconv.Unquote(val)
		if err != nil {
			return "", fmt.Errorf("string: %w", err)
		}
		return s, nil
	}
	return val, nil
}

func tomlBool(val string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	default:
		return false, fmt.Errorf("not a boolean %q", val)
	}
}
