// Package deviceid identifies APRS transmitter devices from parsed packets.
// It uses the community-maintained tocalls.yaml database to determine
// the vendor, model, and class of the transmitting device.
package deviceid

import (
	"fmt"
	"os"
	"strings"

	"github.com/hessu/go-aprs-fap"
	"gopkg.in/yaml.v3"
)

// DeviceID holds the identified device information.
type DeviceID struct {
	Vendor    string   // Manufacturer/author
	Model     string   // Device/software model
	Class     string   // Device class: wx, tracker, rig, ht, app, software, digi, dstar, satellite
	OS        string   // Operating system (optional)
	Version   string   // Version extracted from tocall pattern (optional)
	Features  []string // Feature flags: "messaging", "item-in-msg"
	Messaging bool     // Device supports APRS messaging
}

// DB holds the parsed tocalls.yaml database and lookup structures.
type DB struct {
	fixedTocalls    map[string]*tocallEntry
	wildcardTocalls map[string][]*wildcardTocall // keyed by prefix (2-5 chars)
	miceCodes       map[string]*miceEntry
	miceLegacy      []miceLegacyEntry
}

type tocallEntry struct {
	Vendor   string   `yaml:"vendor"`
	Model    string   `yaml:"model"`
	Class    string   `yaml:"class"`
	OS       string   `yaml:"os"`
	Features []string `yaml:"features"`
}

type wildcardTocall struct {
	pattern string // original pattern string (e.g. "APDnnn", "APnnnU")
	entry   *tocallEntry
}

type miceEntry struct {
	Vendor   string   `yaml:"vendor"`
	Model    string   `yaml:"model"`
	Class    string   `yaml:"class"`
	Features []string `yaml:"features"`
}

type miceLegacyEntry struct {
	Prefix   string   `yaml:"prefix"`
	Suffix   string   `yaml:"suffix"`
	Vendor   string   `yaml:"vendor"`
	Model    string   `yaml:"model"`
	Class    string   `yaml:"class"`
	Features []string `yaml:"features"`
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

type yamlFile struct {
	Tocalls    []yamlTocall     `yaml:"tocalls"`
	Mice       []yamlMice       `yaml:"mice"`
	MiceLegacy []miceLegacyEntry `yaml:"micelegacy"`
}

// isExact returns true if the tocall pattern contains only [A-Z0-9] characters.
func isExact(s string) bool {
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// matchWildcard matches a tocall string against a wildcard pattern.
// Returns (matched, versionStr). The version is the substring matched
// by wildcard characters (n, ?, *). Wildcards:
//
//	n → matches a digit [0-9]
//	? → matches any single character
//	* → matches zero or more characters (greedy, only at end of pattern)
func matchWildcard(pattern, s string) (bool, string) {
	// '*' is only supported at the end of the pattern
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		if len(s) < len(prefix) {
			return false, ""
		}
		// Match the prefix portion
		ok, ver := matchWildcard(prefix, s[:len(prefix)])
		if !ok {
			return false, ""
		}
		// The '*' match plus any remaining chars form the version
		return true, ver + s[len(prefix):]
	}

	if len(pattern) != len(s) {
		return false, ""
	}

	var version strings.Builder
	for i := 0; i < len(pattern); i++ {
		pc := pattern[i]
		sc := s[i]
		switch pc {
		case 'n':
			if sc < '0' || sc > '9' {
				return false, ""
			}
			version.WriteByte(sc)
		case '?':
			version.WriteByte(sc)
		default:
			if pc != sc {
				return false, ""
			}
		}
	}
	return true, version.String()
}

// prefixLen returns the length of the fixed prefix (before any wildcards).
func prefixLen(pattern string) int {
	for i, c := range pattern {
		if c == 'n' || c == '?' || c == '*' {
			return i
		}
	}
	return len(pattern)
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
		fixedTocalls:    make(map[string]*tocallEntry),
		wildcardTocalls: make(map[string][]*wildcardTocall),
		miceCodes:       make(map[string]*miceEntry),
		miceLegacy:      yf.MiceLegacy,
	}

	// Process tocalls
	for _, t := range yf.Tocalls {
		entry := &tocallEntry{
			Vendor:   t.Vendor,
			Model:    t.Model,
			Class:    t.Class,
			OS:       t.OS,
			Features: t.Features,
		}

		if isExact(t.Tocall) {
			db.fixedTocalls[t.Tocall] = entry
		} else {
			plen := prefixLen(t.Tocall)
			prefix := t.Tocall[:plen]
			db.wildcardTocalls[prefix] = append(db.wildcardTocalls[prefix], &wildcardTocall{
				pattern: t.Tocall,
				entry:   entry,
			})
		}
	}

	// Process mice
	for _, m := range yf.Mice {
		db.miceCodes[m.Suffix] = &miceEntry{
			Vendor:   m.Vendor,
			Model:    m.Model,
			Class:    m.Class,
			Features: m.Features,
		}
	}

	return db, nil
}

// Identify determines the device that transmitted the given packet.
// Returns (deviceID, updatedComment, error).
// updatedComment has Mic-E device codes stripped from the comment.
// Returns nil DeviceID and an error if no match is found.
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
		return entryToDeviceID(entry, ""), comment, nil
	}

	// Wildcard tocall match - try prefix lengths 5, 4, 3, 2
	dst := p.DstCallsign
	for plen := 5; plen >= 2; plen-- {
		if plen > len(dst) {
			continue
		}
		prefix := dst[:plen]
		if wts, ok := db.wildcardTocalls[prefix]; ok {
			for _, wt := range wts {
				if matched, version := matchWildcard(wt.pattern, dst); matched {
					return entryToDeviceID(wt.entry, version), comment, nil
				}
			}
		}
	}

	return nil, comment, fmt.Errorf("no device match for tocall %q", p.DstCallsign)
}

func (db *DB) identifyMicE(p *fap.Packet) (*DeviceID, string) {
	comment := p.Comment

	// Try legacy Mic-E patterns (order matters - longer suffixes first)
	for _, leg := range db.miceLegacy {
		if leg.Suffix != "" {
			// Has both prefix and suffix
			if !strings.HasPrefix(comment, leg.Prefix) {
				continue
			}
			if !strings.HasSuffix(comment, leg.Suffix) {
				continue
			}
			// Strip prefix and suffix from comment
			stripped := comment[len(leg.Prefix) : len(comment)-len(leg.Suffix)]
			did := &DeviceID{
				Vendor:    leg.Vendor,
				Model:     leg.Model,
				Class:     leg.Class,
				Features:  leg.Features,
				Messaging: hasFeature(leg.Features, "messaging"),
			}
			return did, stripped
		}
	}

	// Try legacy prefix-only matches
	for _, leg := range db.miceLegacy {
		if leg.Suffix == "" {
			if !strings.HasPrefix(comment, leg.Prefix) {
				continue
			}
			stripped := comment[len(leg.Prefix):]
			did := &DeviceID{
				Vendor:    leg.Vendor,
				Model:     leg.Model,
				Class:     leg.Class,
				Features:  leg.Features,
				Messaging: hasFeature(leg.Features, "messaging"),
			}
			return did, stripped
		}
	}

	// Try new-style Mic-E: prefix char (` or ') followed by data and 2-char suffix
	if len(comment) >= 2 {
		prefix := comment[0]
		suffix := comment[len(comment)-2:]
		if me, ok := db.miceCodes[suffix]; ok {
			did := &DeviceID{
				Vendor:    me.Vendor,
				Model:     me.Model,
				Class:     me.Class,
				Features:  me.Features,
				Messaging: prefix == '`' || hasFeature(me.Features, "messaging"),
			}
			// Strip the suffix from comment
			stripped := comment[:len(comment)-2]
			return did, stripped
		}
	}

	return nil, comment
}

func entryToDeviceID(e *tocallEntry, version string) *DeviceID {
	return &DeviceID{
		Vendor:    e.Vendor,
		Model:     e.Model,
		Class:     e.Class,
		OS:        e.OS,
		Version:   version,
		Features:  e.Features,
		Messaging: hasFeature(e.Features, "messaging"),
	}
}

func hasFeature(features []string, f string) bool {
	for _, feat := range features {
		if feat == f {
			return true
		}
	}
	return false
}
