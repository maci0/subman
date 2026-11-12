// Package store keeps one directory per subscription under the data root.
//
// Layout, for an account id "claude-01":
//
//	data/accounts/claude-01/account.json
//	data/accounts/claude-01/credential.json
//	data/accounts/claude-01/agent/          # the only home a test run sees
//	data/accounts/claude-01/reading.json
//	data/accounts/claude-01/ladder.json
//	data/accounts/claude-01/history.jsonl   # plan changes, appended
//	data/update.json                        # last update check
//	data/settings.json                      # window preferences
//
// Nothing here writes ~/.claude, ~/.codex, ~/.grok, or ~/.gemini.
package store

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maci0/subman/internal/model"
)

const (
	dirPerm  = 0o700
	filePerm = 0o600
	maxCred  = 4 << 20
)

// Root is the data directory. Empty paths become DefaultRoot.
type Root struct {
	Path string
}

// DefaultRoot is the data directory when none was named: $XDG_DATA_HOME/subman
// when that variable is an absolute path, otherwise the platform's own
// per-user data directory.
func DefaultRoot() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "subman"), nil
	}
	base, err := defaultDataBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "subman"), nil
}

func Open(path string) (*Root, error) {
	if path == "" {
		var err error
		path, err = DefaultRoot()
		if err != nil {
			return nil, err
		}
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("data dir must be absolute, got %q (SUBMAN_DATA or -data)", path)
	}
	for _, sub := range []string{"", "accounts"} {
		if err := os.MkdirAll(filepath.Join(path, sub), dirPerm); err != nil {
			return nil, err
		}
	}
	return &Root{Path: path}, nil
}

func (r *Root) accountDir(id string) (string, error) {
	if err := validID(id); err != nil {
		return "", err
	}
	return filepath.Join(r.Path, "accounts", id), nil
}

func validID(id string) error {
	if id == "" || len(id) > 40 || strings.Contains(id, "..") {
		return fmt.Errorf("bad account id %q", id)
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return fmt.Errorf("bad account id %q", id)
		}
	}
	return nil
}

// Accounts reads the accounts directory once. Loaded are the accounts whose
// account.json could be read; problems names the directories it could not,
// each with the reason, so one bad directory does not hide the rest.
func (r *Root) Accounts() (loaded []model.Account, problems []string, err error) {
	entries, err := os.ReadDir(filepath.Join(r.Path, "accounts"))
	if err != nil {
		return nil, nil, err
	}
	loaded = make([]model.Account, 0, len(entries))
	problems = []string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		acc, err := r.Load(e.Name())
		if err != nil {
			reason := "account.json is not valid"
			if errors.Is(err, os.ErrNotExist) {
				reason = "account.json is missing"
			}
			problems = append(problems, fmt.Sprintf("%s: %s", e.Name(), reason))
			continue
		}
		loaded = append(loaded, acc)
	}
	return loaded, problems, nil
}

func (r *Root) List() ([]model.Account, error) {
	loaded, _, err := r.Accounts()
	return loaded, err
}

// Unreadable names every account directory List left out because its
// account.json could not be loaded, each with the reason.
func (r *Root) Unreadable() ([]string, error) {
	_, problems, err := r.Accounts()
	return problems, err
}

func (r *Root) Load(id string) (model.Account, error) {
	dir, err := r.accountDir(id)
	if err != nil {
		return model.Account{}, err
	}
	body, err := os.ReadFile(filepath.Join(dir, "account.json"))
	if err != nil {
		return model.Account{}, err
	}
	if len(body) > maxCred {
		return model.Account{}, fmt.Errorf("%s is over %d bytes", "account.json", maxCred)
	}
	var acc model.Account
	if err := json.Unmarshal(body, &acc); err != nil {
		return model.Account{}, err
	}
	acc.Home = dir
	acc.AgentHome = filepath.Join(dir, "agent")
	// Derived like Home, so a data directory that was moved keeps working.
	acc.CredentialFile = filepath.Join(dir, "credential.json")
	return acc, nil
}

func (r *Root) Save(acc model.Account) error {
	dir, err := r.accountDir(acc.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "agent"), dirPerm); err != nil {
		return err
	}
	// Home fields are derived. Persisting them would let a moved data dir
	// keep pointing at the old one.
	stored := acc
	stored.Home = ""
	stored.AgentHome = ""
	return writeJSON(filepath.Join(dir, "account.json"), stored)
}

// Import copies a credential file into a new account directory.
// The source is read and never written.
func (r *Root) Import(req model.ImportRequest) (model.Account, error) {
	if !req.Provider.Known() {
		return model.Account{}, fmt.Errorf("unknown provider %q", req.Provider)
	}
	body, err := ReadCredential(req.CredentialPath)
	if err != nil {
		return model.Account{}, err
	}
	return r.Create(req, body)
}

// CheckRequest reports what Create would refuse in req, apart from the
// credential: an unknown provider, a name outside 1 to model.MaxNameLen
// characters, a tier the provider does not have (none at all is fine), or a date that does not
// parse. A sign-in calls it first, so a bad field fails before the browser
// opens and not after.
func CheckRequest(req model.ImportRequest) error {
	if !req.Provider.Known() {
		return fmt.Errorf("unknown provider %q", req.Provider)
	}
	if name := strings.TrimSpace(req.Name); name == "" || utf8.RuneCountInString(name) > model.MaxNameLen {
		return fmt.Errorf("name must be 1 to %d characters", model.MaxNameLen)
	}
	// An empty tier is "not known yet": the first good poll may state it.
	if req.TierID != "" && !model.KnownTier(req.Provider, req.TierID) {
		return fmt.Errorf("%s has no tier %q", req.Provider, req.TierID)
	}
	if _, err := ParseTime(req.ActivatedAt); err != nil {
		return fmt.Errorf("activatedAt: %w", err)
	}
	if _, err := ParseTime(req.RenewalAt); err != nil {
		return fmt.Errorf("renewalAt: %w", err)
	}
	return nil
}

// Create makes a new account directory that holds credential as its
// credential.json: what a sign-in returned, or the bytes Import read.
// req.CredentialPath is not used.
func (r *Root) Create(req model.ImportRequest, body []byte) (model.Account, error) {
	if err := CheckRequest(req); err != nil {
		return model.Account{}, err
	}
	name := strings.TrimSpace(req.Name)
	if len(body) == 0 || len(body) > maxCred {
		return model.Account{}, fmt.Errorf("credential is empty or over %d bytes", maxCred)
	}

	id, err := newID(req.Provider)
	if err != nil {
		return model.Account{}, err
	}
	dir, err := r.accountDir(id)
	if err != nil {
		return model.Account{}, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "agent"), dirPerm); err != nil {
		return model.Account{}, err
	}
	dstPath := filepath.Join(dir, "credential.json")
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
	if err != nil {
		return model.Account{}, err
	}
	if _, err := dst.Write(body); err != nil {
		dst.Close()
		os.RemoveAll(dir)
		return model.Account{}, err
	}
	if err := dst.Close(); err != nil {
		os.RemoveAll(dir)
		return model.Account{}, err
	}

	acc := model.Account{
		ID:             id,
		Name:           name,
		Provider:       req.Provider,
		Created:        time.Now().UTC(),
		CredentialFile: dstPath,
		TierID:         req.TierID,
	}
	if t, err := ParseTime(req.ActivatedAt); err == nil {
		acc.ActivatedAt = t
	} else if req.ActivatedAt != "" {
		os.RemoveAll(dir)
		return model.Account{}, fmt.Errorf("activatedAt: %w", err)
	}
	if t, err := ParseTime(req.RenewalAt); err == nil {
		acc.RenewalAt = t
	} else if req.RenewalAt != "" {
		os.RemoveAll(dir)
		return model.Account{}, fmt.Errorf("renewalAt: %w", err)
	}
	if err := r.Save(acc); err != nil {
		os.RemoveAll(dir)
		return model.Account{}, err
	}
	return r.Load(id)
}

// ReadCredential reads a credential file the operator pointed at. The path
// must be absolute and name a regular, non-empty file of at most maxCred bytes.
func ReadCredential(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("credential path must be absolute")
	}
	src, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open credential: %w", err)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxCred {
		return nil, fmt.Errorf("credential file is empty, not a file, or over %d bytes", maxCred)
	}
	return io.ReadAll(io.LimitReader(src, maxCred))
}

// ReplaceCredential copies a new credential file over the account's own
// copy, for a login that can no longer be renewed. The source is read and
// never written.
func (r *Root) ReplaceCredential(id, path string) error {
	body, err := ReadCredential(path)
	if err != nil {
		return err
	}
	return r.PutCredential(id, body)
}

// PutCredential replaces the account's credential copy with credential: a
// fresh sign-in, or the bytes ReplaceCredential read. The copy lands through
// a rename, so a failed write leaves the old credential in place.
func (r *Root) PutCredential(id string, credential []byte) error {
	acc, err := r.Load(id)
	if err != nil {
		return err
	}
	if len(credential) == 0 || len(credential) > maxCred {
		return fmt.Errorf("credential is empty or over %d bytes", maxCred)
	}
	tmp := acc.CredentialFile + ".tmp"
	if err := os.WriteFile(tmp, credential, filePerm); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, acc.CredentialFile); err != nil {
		// The previous credential is untouched. Drop the temp so a later
		// replacement cannot rename this half-written copy into place.
		os.Remove(tmp)
		return err
	}
	return nil
}

func (r *Root) Delete(id string) error {
	dir, err := r.accountDir(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// PutReading stores a poll. A failed poll that carries no meters keeps the
// meters, credits, and plan of the last reading and marks them stale, so one
// bad poll does not blank the account. OK stays false and Error stays set,
// and the ladder never acts on a reading that is not OK.
func (r *Root) PutReading(id string, reading model.Reading) error {
	dir, err := r.accountDir(id)
	if err != nil {
		return err
	}
	if !reading.OK && len(reading.Windows) == 0 {
		// An unreadable previous file is not carried over; the failure is stored as it came.
		if prev, ok, err := r.Reading(id); err == nil && ok && len(prev.Windows) > 0 {
			reading.Windows = prev.Windows
			reading.Credits = prev.Credits
			reading.Plan = prev.Plan
			reading.TierID = prev.TierID
			reading.Stale = true
		}
	}
	return writeJSON(filepath.Join(dir, "reading.json"), reading)
}

// maxHistory bounds how many plan changes History returns.
const maxHistory = 50

// AppendHistory records one attempted plan change in the account's
// history.jsonl, one JSON object per line, oldest first.
func (r *Root) AppendHistory(id string, entry model.HistoryEntry) error {
	dir, err := r.accountDir(id)
	if err != nil {
		return err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "history.jsonl")
	// The trim rewrites the whole file. An exclusive lock held across the
	// append and that rewrite stops a second process, which shares this
	// directory (the window and the CLI), from appending a line the trim
	// never saw and then losing it when the rewrite is renamed into place.
	unlock, err := lockHistory(path)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePerm)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	// History only returns maxHistory lines, but the file was append-only, so
	// a long-lived account grew it without bound. The line above is durable;
	// a trim that fails is retried on the next append and is not this entry's
	// error, or a saved step would be reported as unrecorded.
	_ = trimHistory(path)
	return nil
}

// lockHistory takes an exclusive lock on path, creating it if needed, and
// returns the function that releases it. The lock lives on a sidecar so the
// history file itself can be replaced by rename without the locker keeping
// the old inode open: a lock on the history descriptor would survive the
// rename and then guard a file the directory no longer names.
func lockHistory(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// trimHistory rewrites path down to its last maxHistory lines once it has
// grown past that. Lines that are not JSON are dropped, matching History,
// which skips them. The caller holds the history lock, so no append lands
// between the read and the rename that replaces the file.
func trimHistory(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var kept []string
	for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
		if line == "" || !json.Valid([]byte(line)) {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) <= maxHistory {
		return nil
	}
	kept = kept[len(kept)-maxHistory:]
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(kept, "\n")+"\n"), filePerm); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// The untrimmed file is still complete. A leftover tmp would be
		// renamed over it by the next trim that fails the same way.
		os.Remove(tmp)
		return err
	}
	return nil
}

// History returns the account's last maxHistory plan changes, newest first.
// An account that never stepped has none.
//
// AppendHistory keeps the file at maxHistory lines. A file that grew past
// that before it was trimmed is still only decoded at its tail:
// each line is one JSON object, and a line over maxLine cannot be one, so
// maxHistory of them fit in the window that is read.
func (r *Root) History(id string) ([]model.HistoryEntry, error) {
	dir, err := r.accountDir(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, "history.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// One extra line: the seek lands inside a line, and that fragment is dropped.
	const maxLine = 8192
	window := int64(maxHistory+1) * maxLine
	if info.Size() > window {
		if _, err := f.Seek(info.Size()-window, io.SeekStart); err != nil {
			return nil, err
		}
	}
	out := make([]model.HistoryEntry, 0, maxHistory)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, maxLine), maxLine)
	first := info.Size() > window
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			// The seek lands mid-line, so the first fragment is not a record.
			first = false
			continue
		}
		if line == "" {
			continue
		}
		var entry model.HistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// A torn append (crash, full disk) leaves one bad line. It is
			// skipped so the rest of the history, and the account, still load.
			continue
		}
		out = append(out, entry)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) > maxHistory {
		out = out[len(out)-maxHistory:]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (r *Root) Reading(id string) (model.Reading, bool, error) {
	dir, err := r.accountDir(id)
	if err != nil {
		return model.Reading{}, false, err
	}
	var reading model.Reading
	err = readJSON(filepath.Join(dir, "reading.json"), &reading)
	if errors.Is(err, os.ErrNotExist) {
		return model.Reading{}, false, nil
	}
	if err != nil {
		return model.Reading{}, false, err
	}
	return reading, true, nil
}

func (r *Root) PutLadder(id string, st model.LadderState) error {
	dir, err := r.accountDir(id)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "ladder.json"), st)
}

func (r *Root) Ladder(id string) (model.LadderState, error) {
	dir, err := r.accountDir(id)
	if err != nil {
		return model.LadderState{}, err
	}
	var st model.LadderState
	err = readJSON(filepath.Join(dir, "ladder.json"), &st)
	if errors.Is(err, os.ErrNotExist) {
		return model.LadderState{Phase: model.PhaseOff}, nil
	}
	return st, err
}

func (r *Root) PutUpdate(st model.UpdateStatus) error {
	return writeJSON(filepath.Join(r.Path, "update.json"), st)
}

func (r *Root) Update() (model.UpdateStatus, error) {
	var st model.UpdateStatus
	err := readJSON(filepath.Join(r.Path, "update.json"), &st)
	if errors.Is(err, os.ErrNotExist) {
		return model.UpdateStatus{}, nil
	}
	return st, err
}

// PutSettings stores the window's preferences after validating them.
func (r *Root) PutSettings(s model.Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	return writeJSON(filepath.Join(r.Path, "settings.json"), s)
}

// Settings returns the stored preferences, or the defaults when none were
// saved. A stored file that no longer validates is an error, not a reset.
// Every field must be present: decoding into the defaults would start the
// 15-minute poll, or turn notifications on, from a file that never asked.
func (r *Root) Settings() (model.Settings, error) {
	path := filepath.Join(r.Path, "settings.json")
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.DefaultSettings(), nil
	}
	if err != nil {
		return model.Settings{}, err
	}
	if len(body) > maxCred {
		return model.Settings{}, fmt.Errorf("%s is over %d bytes", filepath.Base(path), maxCred)
	}
	var raw struct {
		AutoPollMinutes *int              `json:"autoPollMinutes"`
		Notify          *bool             `json:"notify"`
		FoldedProviders *[]model.Provider `json:"foldedProviders"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return model.Settings{}, fmt.Errorf("settings.json: %w", err)
	}
	// Decode stops at the object, so a second value would be ignored.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("extra data after the settings object")
		}
		return model.Settings{}, fmt.Errorf("settings.json: %w", err)
	}
	switch {
	case raw.AutoPollMinutes == nil:
		return model.Settings{}, fmt.Errorf("settings.json is missing autoPollMinutes")
	case raw.Notify == nil:
		return model.Settings{}, fmt.Errorf("settings.json is missing notify")
	case raw.FoldedProviders == nil:
		return model.Settings{}, fmt.Errorf("settings.json is missing foldedProviders")
	}
	s := model.Settings{
		AutoPollMinutes: *raw.AutoPollMinutes,
		Notify:          *raw.Notify,
		FoldedProviders: *raw.FoldedProviders,
	}
	return s, s.Validate()
}

func newID(p model.Provider) (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return string(p)[:1] + hex.EncodeToString(b[:]), nil
}

// ParseTime reads an RFC 3339 instant or a YYYY-MM-DD day as UTC. An empty
// string is no time: it returns nil and no error.
func ParseTime(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		t, err := time.Parse(layout, s)
		if err == nil {
			u := t.UTC()
			return &u, nil
		}
	}
	return nil, fmt.Errorf("want RFC3339 or YYYY-MM-DD, got %q", s)
}

func writeJSON(path string, v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, filePerm); err != nil {
		// A short write still creates the file. Leaving it would make the
		// next save rename a partial document over the real one.
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func readJSON(path string, v any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(body) > maxCred {
		return fmt.Errorf("%s is over %d bytes", filepath.Base(path), maxCred)
	}
	return json.Unmarshal(body, v)
}
