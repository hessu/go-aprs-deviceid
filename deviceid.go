// Package deviceid identifies APRS transmitter devices from parsed packets.
// It uses the community-maintained tocalls.yaml database to determine
// the vendor, model, and class of the transmitting device.
package deviceid

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"

	"github.com/hessu/go-aprs-fap"
	"gopkg.in/yaml.v3"
)

// ErrNoMatch is returned when no device match is found for a packet.
var ErrNoMatch = errors.New("no device match")

// DeviceID holds the identified device information.
type DeviceID struct {
	Vendor    string   // Manufacturer/author
	Model     string   // Device/software model
	Class     string   // Device class: wx, tracker, rig, ht, app, software, digi, dstar, satellite
	OS        string   // Operating system (optional)
	Features  []string // Feature flags: "messaging", "item-in-msg"
	Messaging bool     // Device supports APRS messaging
}

// DeviceDB is a concurrency-safe device identification database that
// supports hot-reloading. Use Open to create one, then call Identify
// from any number of goroutines. Call CheckReload periodically to
// pick up changes to the underlying YAML file.
type DeviceDB struct {
	path string
	ino  uint64
	mtime syscall.Timespec
	db   atomic.Pointer[db]
}

// db holds the parsed tocalls.yaml lookup structures. Immutable after construction.
type db struct {
	fixedTocalls    map[string]*DeviceID
	wildcardTocalls map[string][]wildcardTocall // keyed by prefix (2-5 chars)
	miceCodes       map[string]*miceDeviceIDs
	miceLegacySuf   []miceLegacyEntry // entries with both prefix and suffix
	miceLegacyPre   []miceLegacyEntry // entries with prefix only
}

type wildcardTocall struct {
	pattern string // original pattern string (e.g. "APDnnn", "APnnnU")
	id      *DeviceID
}

type miceLegacyEntry struct {
	Prefix byte
	Suffix byte // 0 if prefix-only
	id     *DeviceID
}

// miceDeviceIDs holds two precomputed DeviceIDs for new-style Mic-E codes,
// since the Messaging flag depends on the runtime prefix character.
type miceDeviceIDs struct {
	withMsg    DeviceID
	withoutMsg DeviceID
}

type yamlTocall struct {
	Tocall   string   `yaml:"tocall"`
	Vendor   string   `yaml:"vendor"`
	Model    string   `yaml:"model"`
	Class    string   `yaml:"class"`
	OS       string   `yaml:"os"`
	Features []string `yaml:"features"`
}

type yamlMice struct {
	Suffix   string   `yaml:"suffix"`
	Vendor   string   `yaml:"vendor"`
	Model    string   `yaml:"model"`
	Class    string   `yaml:"class"`
	Features []string `yaml:"features"`
}

type yamlMiceLegacy struct {
	Prefix   string   `yaml:"prefix"`
	Suffix   string   `yaml:"suffix"`
	Vendor   string   `yaml:"vendor"`
	Model    string   `yaml:"model"`
	Class    string   `yaml:"class"`
	Features []string `yaml:"features"`
}

type yamlFile struct {
	Tocalls    []yamlTocall    `yaml:"tocalls"`
	Mice       []yamlMice      `yaml:"mice"`
	MiceLegacy []yamlMiceLegacy `yaml:"micelegacy"`
}

// isExact returns true if the tocall pattern contains only [A-Z0-9] characters.
func isExact(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// matchWildcard matches a tocall string against a wildcard pattern.
// Wildcards: n → digit [0-9], ? → any single character,
// * → zero or more characters (only at end of pattern).
func matchWildcard(pattern, s string) bool {
	hasStar := len(pattern) > 0 && pattern[len(pattern)-1] == '*'

	patLen := len(pattern)
	if hasStar {
		patLen--
		if len(s) < patLen {
			return false
		}
	} else if len(s) != patLen {
		return false
	}

	for i := 0; i < patLen; i++ {
		pc := pattern[i]
		sc := s[i]
		switch pc {
		case 'n':
			if sc < '0' || sc > '9' {
				return false
			}
		case '?':
			// matches anything
		default:
			if pc != sc {
				return false
			}
		}
	}
	return true
}

// prefixLen returns the length of the fixed prefix (before any wildcards).
func prefixLen(pattern string) int {
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == 'n' || c == '?' || c == '*' {
			return i
		}
	}
	return len(pattern)
}

func hasFeature(features []string, f string) bool {
	for _, feat := range features {
		if feat == f {
			return true
		}
	}
	return false
}

// Open loads a tocalls.yaml file and returns a DeviceDB ready for concurrent use.
// Call Identify to look up devices, and CheckReload to pick up file changes.
func Open(yamlPath string) (*DeviceDB, error) {
	ddb := &DeviceDB{path: yamlPath}
	if err := ddb.load(); err != nil {
		return nil, err
	}
	return ddb, nil
}

// load reads and parses the YAML file, updates the stored inode/mtime,
// and atomically swaps in the new db.
func (ddb *DeviceDB) load() error {
	var st syscall.Stat_t
	if err := syscall.Stat(ddb.path, &st); err != nil {
		return fmt.Errorf("stat %s: %w", ddb.path, err)
	}

	d, err := loadDB(ddb.path)
	if err != nil {
		return err
	}

	ddb.ino = st.Ino
	ddb.mtime = st.Mtim
	ddb.db.Store(d)
	return nil
}

// CheckReload checks whether the database file has changed (by comparing
// inode and mtime) and reloads it if so. An optional path argument switches
// the database to a different YAML file. Returns true if the database was
// reloaded. It is safe to call from a single goroutine while other
// goroutines call Identify concurrently.
func (ddb *DeviceDB) CheckReload(path ...string) (bool, error) {
	if len(path) > 0 && path[0] != "" {
		ddb.path = path[0]
	}

	var st syscall.Stat_t
	if err := syscall.Stat(ddb.path, &st); err != nil {
		return false, fmt.Errorf("stat %s: %w", ddb.path, err)
	}

	if st.Ino == ddb.ino && st.Mtim == ddb.mtime {
		return false, nil
	}

	if err := ddb.load(); err != nil {
		return false, err
	}
	return true, nil
}

// loadDB reads and parses a tocalls.yaml file, building lookup structures.
func loadDB(yamlPath string) (*db, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("reading tocalls.yaml: %w", err)
	}

	var yf yamlFile
	if err := yaml.Unmarshal(data, &yf); err != nil {
		return nil, fmt.Errorf("parsing tocalls.yaml: %w", err)
	}

	d := &db{
		fixedTocalls:    make(map[string]*DeviceID, len(yf.Tocalls)),
		wildcardTocalls: make(map[string][]wildcardTocall),
		miceCodes:       make(map[string]*miceDeviceIDs, len(yf.Mice)),
	}

	// Process tocalls
	for _, t := range yf.Tocalls {
		did := &DeviceID{
			Vendor:    t.Vendor,
			Model:     t.Model,
			Class:     t.Class,
			OS:        t.OS,
			Features:  t.Features,
			Messaging: hasFeature(t.Features, "messaging"),
		}

		if isExact(t.Tocall) {
			d.fixedTocalls[t.Tocall] = did
		} else {
			plen := prefixLen(t.Tocall)
			prefix := t.Tocall[:plen]
			d.wildcardTocalls[prefix] = append(d.wildcardTocalls[prefix], wildcardTocall{
				pattern: t.Tocall,
				id:      did,
			})
		}
	}

	// Process mice — precompute both messaging variants
	for _, m := range yf.Mice {
		base := DeviceID{
			Vendor:   m.Vendor,
			Model:    m.Model,
			Class:    m.Class,
			Features: m.Features,
		}
		ids := &miceDeviceIDs{withoutMsg: base, withMsg: base}
		ids.withMsg.Messaging = true
		if hasFeature(m.Features, "messaging") {
			ids.withoutMsg.Messaging = true
		}
		d.miceCodes[m.Suffix] = ids
	}

	// Process mic-e legacy — split into suffix and prefix-only slices
	for _, m := range yf.MiceLegacy {
		did := &DeviceID{
			Vendor:    m.Vendor,
			Model:     m.Model,
			Class:     m.Class,
			Features:  m.Features,
			Messaging: hasFeature(m.Features, "messaging"),
		}
		e := miceLegacyEntry{id: did}
		if len(m.Prefix) > 0 {
			e.Prefix = m.Prefix[0]
		}
		if m.Suffix != "" {
			e.Suffix = m.Suffix[0]
			d.miceLegacySuf = append(d.miceLegacySuf, e)
		} else {
			d.miceLegacyPre = append(d.miceLegacyPre, e)
		}
	}

	return d, nil
}

// Identify determines the device that transmitted the given packet.
// It is safe to call concurrently from multiple goroutines.
// Returns (deviceID, updatedComment, error).
// updatedComment has Mic-E device codes stripped from the comment.
// Returns nil DeviceID and ErrNoMatch if no match is found.
func (ddb *DeviceDB) Identify(p *fap.Packet) (*DeviceID, string, error) {
	return ddb.db.Load().identify(p)
}

func (d *db) identify(p *fap.Packet) (*DeviceID, string, error) {
	if p.Format == "" || p.DstCallsign == "" {
		return nil, p.Comment, fmt.Errorf("packet has no format or destination callsign")
	}

	comment := p.Comment

	// Mic-E format
	if p.Format == fap.FormatMicE {
		did, updatedComment := d.identifyMicE(p)
		if did != nil {
			return did, updatedComment, nil
		}
	}

	// Exact tocall match
	if did, ok := d.fixedTocalls[p.DstCallsign]; ok {
		return did, comment, nil
	}

	// Wildcard tocall match - try prefix lengths 5, 4, 3, 2
	dst := p.DstCallsign
	for plen := 5; plen >= 2; plen-- {
		if plen > len(dst) {
			continue
		}
		prefix := dst[:plen]
		if wts, ok := d.wildcardTocalls[prefix]; ok {
			for i := range wts {
				if matchWildcard(wts[i].pattern, dst) {
					return wts[i].id, comment, nil
				}
			}
		}
	}

	return nil, comment, ErrNoMatch
}

func (d *db) identifyMicE(p *fap.Packet) (*DeviceID, string) {
	comment := p.Comment
	if len(comment) < 1 {
		return nil, comment
	}

	first := comment[0]

	// Try legacy Mic-E patterns with both prefix and suffix
	for i := range d.miceLegacySuf {
		leg := &d.miceLegacySuf[i]
		if len(comment) < 2 || first != leg.Prefix || comment[len(comment)-1] != leg.Suffix {
			continue
		}
		return leg.id, comment[1 : len(comment)-1]
	}

	// Try legacy prefix-only matches
	for i := range d.miceLegacyPre {
		leg := &d.miceLegacyPre[i]
		if first != leg.Prefix {
			continue
		}
		return leg.id, comment[1:]
	}

	// Try new-style Mic-E: 2-char suffix lookup
	if len(comment) >= 2 {
		suffix := comment[len(comment)-2:]
		if ids, ok := d.miceCodes[suffix]; ok {
			if first == '`' {
				return &ids.withMsg, comment[:len(comment)-2]
			}
			return &ids.withoutMsg, comment[:len(comment)-2]
		}
	}

	return nil, comment
}
