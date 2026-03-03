// Package deviceid identifies APRS transmitter devices from parsed packets.
// It uses the community-maintained tocalls.yaml database to determine
// the vendor, model, and class of the transmitting device.
package deviceid

import (
	"errors"
	"fmt"
	"os"

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

// DB holds the parsed tocalls.yaml database and lookup structures.
type DB struct {
	fixedTocalls    map[string]*tocallEntry
	wildcardTocalls map[string][]wildcardTocall // keyed by prefix (2-5 chars)
	miceCodes       map[string]*miceEntry
	miceLegacySuf   []miceLegacyEntry // entries with both prefix and suffix
	miceLegacyPre   []miceLegacyEntry // entries with prefix only
}

type tocallEntry struct {
	Vendor    string
	Model     string
	Class     string
	OS        string
	Features  []string
	Messaging bool
}

type wildcardTocall struct {
	pattern string // original pattern string (e.g. "APDnnn", "APnnnU")
	entry   *tocallEntry
}

type miceEntry struct {
	Vendor    string
	Model     string
	Class     string
	Features  []string
	Messaging bool
}

type miceLegacyEntry struct {
	Prefix    string
	Suffix    string
	Vendor    string
	Model     string
	Class     string
	Features  []string
	Messaging bool
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

// LoadDB loads and parses a tocalls.yaml file, building lookup structures.
func LoadDB(yamlPath string) (*DB, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("reading tocalls.yaml: %w", err)
	}

	var yf yamlFile
	if err := yaml.Unmarshal(data, &yf); err != nil {
		return nil, fmt.Errorf("parsing tocalls.yaml: %w", err)
	}

	db := &DB{
		fixedTocalls:    make(map[string]*tocallEntry, len(yf.Tocalls)),
		wildcardTocalls: make(map[string][]wildcardTocall),
		miceCodes:       make(map[string]*miceEntry, len(yf.Mice)),
	}

	// Process tocalls
	for _, t := range yf.Tocalls {
		entry := &tocallEntry{
			Vendor:    t.Vendor,
			Model:     t.Model,
			Class:     t.Class,
			OS:        t.OS,
			Features:  t.Features,
			Messaging: hasFeature(t.Features, "messaging"),
		}

		if isExact(t.Tocall) {
			db.fixedTocalls[t.Tocall] = entry
		} else {
			plen := prefixLen(t.Tocall)
			prefix := t.Tocall[:plen]
			db.wildcardTocalls[prefix] = append(db.wildcardTocalls[prefix], wildcardTocall{
				pattern: t.Tocall,
				entry:   entry,
			})
		}
	}

	// Process mice
	for _, m := range yf.Mice {
		db.miceCodes[m.Suffix] = &miceEntry{
			Vendor:    m.Vendor,
			Model:     m.Model,
			Class:     m.Class,
			Features:  m.Features,
			Messaging: hasFeature(m.Features, "messaging"),
		}
	}

	// Process mic-e legacy — split into suffix and prefix-only slices
	for _, m := range yf.MiceLegacy {
		e := miceLegacyEntry{
			Prefix:    m.Prefix,
			Suffix:    m.Suffix,
			Vendor:    m.Vendor,
			Model:     m.Model,
			Class:     m.Class,
			Features:  m.Features,
			Messaging: hasFeature(m.Features, "messaging"),
		}
		if m.Suffix != "" {
			db.miceLegacySuf = append(db.miceLegacySuf, e)
		} else {
			db.miceLegacyPre = append(db.miceLegacyPre, e)
		}
	}

	return db, nil
}

// Identify determines the device that transmitted the given packet.
// Returns (deviceID, updatedComment, error).
// updatedComment has Mic-E device codes stripped from the comment.
// Returns nil DeviceID and ErrNoMatch if no match is found.
func (db *DB) Identify(p *fap.Packet) (*DeviceID, string, error) {
	if p.Format == "" && p.DstCallsign == "" {
		return nil, p.Comment, fmt.Errorf("packet has no format or destination callsign")
	}

	comment := p.Comment

	// Mic-E format
	if p.Format == fap.FormatMicE {
		did, updatedComment := db.identifyMicE(p)
		if did != nil {
			return did, updatedComment, nil
		}
	}

	// Exact tocall match
	if entry, ok := db.fixedTocalls[p.DstCallsign]; ok {
		return entryToDeviceID(entry), comment, nil
	}

	// Wildcard tocall match - try prefix lengths 5, 4, 3, 2
	dst := p.DstCallsign
	for plen := 5; plen >= 2; plen-- {
		if plen > len(dst) {
			continue
		}
		prefix := dst[:plen]
		if wts, ok := db.wildcardTocalls[prefix]; ok {
			for i := range wts {
				if matchWildcard(wts[i].pattern, dst) {
					return entryToDeviceID(wts[i].entry), comment, nil
				}
			}
		}
	}

	return nil, comment, ErrNoMatch
}

func (db *DB) identifyMicE(p *fap.Packet) (*DeviceID, string) {
	comment := p.Comment

	// Try legacy Mic-E patterns with both prefix and suffix
	for i := range db.miceLegacySuf {
		leg := &db.miceLegacySuf[i]
		if len(comment) < len(leg.Prefix)+len(leg.Suffix) {
			continue
		}
		if comment[0] != leg.Prefix[0] {
			continue
		}
		if comment[len(comment)-1] != leg.Suffix[len(leg.Suffix)-1] {
			continue
		}
		if len(leg.Prefix) > 1 && comment[:len(leg.Prefix)] != leg.Prefix {
			continue
		}
		if len(leg.Suffix) > 1 && comment[len(comment)-len(leg.Suffix):] != leg.Suffix {
			continue
		}
		stripped := comment[len(leg.Prefix) : len(comment)-len(leg.Suffix)]
		return &DeviceID{
			Vendor:    leg.Vendor,
			Model:     leg.Model,
			Class:     leg.Class,
			Features:  leg.Features,
			Messaging: leg.Messaging,
		}, stripped
	}

	// Try legacy prefix-only matches
	for i := range db.miceLegacyPre {
		leg := &db.miceLegacyPre[i]
		if len(comment) < len(leg.Prefix) {
			continue
		}
		if comment[0] != leg.Prefix[0] {
			continue
		}
		if len(leg.Prefix) > 1 && comment[:len(leg.Prefix)] != leg.Prefix {
			continue
		}
		stripped := comment[len(leg.Prefix):]
		return &DeviceID{
			Vendor:    leg.Vendor,
			Model:     leg.Model,
			Class:     leg.Class,
			Features:  leg.Features,
			Messaging: leg.Messaging,
		}, stripped
	}

	// Try new-style Mic-E: 2-char suffix lookup
	if len(comment) >= 2 {
		prefix := comment[0]
		suffix := comment[len(comment)-2:]
		if me, ok := db.miceCodes[suffix]; ok {
			return &DeviceID{
				Vendor:    me.Vendor,
				Model:     me.Model,
				Class:     me.Class,
				Features:  me.Features,
				Messaging: prefix == '`' || me.Messaging,
			}, comment[:len(comment)-2]
		}
	}

	return nil, comment
}

func entryToDeviceID(e *tocallEntry) *DeviceID {
	return &DeviceID{
		Vendor:    e.Vendor,
		Model:     e.Model,
		Class:     e.Class,
		OS:        e.OS,
		Features:  e.Features,
		Messaging: e.Messaging,
	}
}
