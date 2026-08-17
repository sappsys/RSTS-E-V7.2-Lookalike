package rsts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Account struct {
	Proj       int    `json:"proj"`
	Prog       int    `json:"prog"`
	Name       string `json:"name"`
	Password   string `json:"password"`
	Privileged bool   `json:"privileged"`
}

func (a Account) PPN() string     { return fmt.Sprintf("%d,%d", a.Proj, a.Prog) }
func (a Account) Display() string { return fmt.Sprintf("[%d,%d]", a.Proj, a.Prog) }

func (a Account) Matches(token string) bool {
	t := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(token), " ", ""))
	t = strings.ReplaceAll(strings.ReplaceAll(t, "[", ""), "]", "")
	if t == strings.ToUpper(a.Name) {
		return true
	}
	if t == a.PPN() {
		return true
	}
	if t == fmt.Sprintf("%d/%d", a.Proj, a.Prog) {
		return true
	}
	return false
}

type accountFile struct {
	Accounts []Account `json:"accounts"`
}

var defaultAccounts = []Account{
	{Proj: 1, Prog: 2, Name: "SYSTEM", Password: "SYSTEM", Privileged: true},
	{Proj: 100, Prog: 100, Name: "GUEST", Password: "GUEST"},
	{Proj: 200, Prog: 200, Name: "DEMO", Password: "DEMO"},
}

type AccountDB struct {
	mu       sync.Mutex
	Path     string
	Accounts []*Account
}

func OpenAccountDB(path string) (*AccountDB, error) {
	db := &AccountDB{Path: path}
	if err := db.Load(); err != nil {
		return nil, err
	}
	return db, nil
}

func cloneDefaults() []*Account {
	out := make([]*Account, len(defaultAccounts))
	for i, a := range defaultAccounts {
		c := a
		out[i] = &c
	}
	return out
}

func (db *AccountDB) snapshot() []Account {
	out := make([]Account, len(db.Accounts))
	for i, a := range db.Accounts {
		if a != nil {
			out[i] = *a
		}
	}
	return out
}

func (db *AccountDB) Load() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	data, err := os.ReadFile(db.Path)
	if err != nil {
		if os.IsNotExist(err) {
			db.Accounts = cloneDefaults()
			return db.saveLocked()
		}
		return err
	}
	var file accountFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	if len(file.Accounts) == 0 {
		db.Accounts = cloneDefaults()
		return db.saveLocked()
	}
	db.Accounts = make([]*Account, len(file.Accounts))
	for i := range file.Accounts {
		a := file.Accounts[i]
		db.Accounts[i] = &a
	}
	return nil
}

func (db *AccountDB) Save() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.saveLocked()
}

func (db *AccountDB) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(db.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(accountFile{Accounts: db.snapshot()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(db.Path, append(data, '\n'), 0o644)
}

func (db *AccountDB) findLocked(token string) *Account {
	for _, a := range db.Accounts {
		if a != nil && a.Matches(token) {
			return a
		}
	}
	return nil
}

func (db *AccountDB) Find(token string) *Account {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.findLocked(token)
}

func (db *AccountDB) findPPNLocked(proj, prog int) *Account {
	for _, a := range db.Accounts {
		if a != nil && a.Proj == proj && a.Prog == prog {
			return a
		}
	}
	return nil
}

func (db *AccountDB) FindPPN(proj, prog int) *Account {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.findPPNLocked(proj, prog)
}

func (db *AccountDB) Authenticate(token, password string) *Account {
	db.mu.Lock()
	defer db.mu.Unlock()
	acct := db.findLocked(token)
	if acct == nil {
		return nil
	}
	if strings.ToUpper(acct.Password) != strings.ToUpper(strings.TrimSpace(password)) {
		return nil
	}
	return acct
}

func (db *AccountDB) Create(proj, prog int, name, password string, privileged bool) (*Account, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if proj < 0 || proj > 254 || prog < 0 || prog > 255 {
		return nil, fmt.Errorf("Illegal PPN")
	}
	if db.findPPNLocked(proj, prog) != nil {
		return nil, fmt.Errorf("Account already exists")
	}
	n, err := normalizeAccountName(name)
	if err != nil {
		return nil, err
	}
	pw, err := normalizePassword(password)
	if err != nil {
		return nil, err
	}
	for _, a := range db.Accounts {
		if a != nil && strings.EqualFold(a.Name, n) {
			return nil, fmt.Errorf("Name already in use")
		}
	}
	acct := &Account{
		Proj:       proj,
		Prog:       prog,
		Name:       n,
		Password:   pw,
		Privileged: privileged || proj == 1,
	}
	db.Accounts = append(db.Accounts, acct)
	if err := db.saveLocked(); err != nil {
		return nil, err
	}
	return acct, nil
}

func (db *AccountDB) SetPassword(acct *Account, password string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if acct == nil {
		return fmt.Errorf("No account")
	}
	pw, err := normalizePassword(password)
	if err != nil {
		return err
	}
	target := db.findPPNLocked(acct.Proj, acct.Prog)
	if target == nil {
		return fmt.Errorf("Can't find file or account")
	}
	target.Password = pw
	acct.Password = pw
	return db.saveLocked()
}

func (db *AccountDB) Delete(proj, prog int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if proj == 1 && prog == 2 {
		return fmt.Errorf("Protection violation")
	}
	idx := -1
	for i, a := range db.Accounts {
		if a != nil && a.Proj == proj && a.Prog == prog {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("Can't find file or account")
	}
	db.Accounts = append(db.Accounts[:idx], db.Accounts[idx+1:]...)
	return db.saveLocked()
}

func (db *AccountDB) List() []Account {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.snapshot()
}

func ParsePPN(text string) (int, int, error) {
	s := strings.ToUpper(strings.TrimSpace(text))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "[", ""), "]", "")
	s = strings.ReplaceAll(s, "/", ",")
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("Illegal PPN")
	}
	proj, err1 := strconv.Atoi(parts[0])
	prog, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("Illegal PPN")
	}
	if proj < 0 || proj > 254 || prog < 0 || prog > 255 {
		return 0, 0, fmt.Errorf("Illegal PPN")
	}
	return proj, prog, nil
}

func normalizeAccountName(name string) (string, error) {
	n := strings.ToUpper(strings.TrimSpace(name))
	if n == "" {
		return "", fmt.Errorf("Illegal account name")
	}
	if len(n) > 9 {
		return "", fmt.Errorf("Illegal account name")
	}
	for _, r := range n {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '$' {
			return "", fmt.Errorf("Illegal account name")
		}
	}
	return n, nil
}

func normalizePassword(password string) (string, error) {
	pw := strings.ToUpper(strings.TrimSpace(password))
	if pw == "" {
		return "", fmt.Errorf("Illegal password")
	}
	if len(pw) > 16 {
		return "", fmt.Errorf("Illegal password")
	}
	for _, r := range pw {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '$' {
			return "", fmt.Errorf("Illegal password")
		}
	}
	return pw, nil
}
