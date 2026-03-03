// Package deviceid identifies APRS transmitter devices from parsed packets.
// It uses the community-maintained tocalls.yaml database to determine
// the vendor, model, and class of the transmitting device.
package deviceid

import (
	"fmt"
	"os"
	"regexp"
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
	fixedTocalls  map[string]*tocallEntry
	regexpTocalls map[string][]*regexpTocall // keyed by prefix (2-5 chars)
	miceCodes     map[string]*miceEntry
	miceLegacy    []miceLegacyEntry
}

type tocallEntry struct {
	Vendor   string   `yaml:"vendor"`
	Model    string   `yaml:"model"`
	Class    string   `yaml:"class"`
	OS       string   `yaml:"os"`
	Features []string `yaml:"features"`
}

type regexpTocall struct {
	re    *regexp.Regexp
	entry *tocallEntry
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

// tocallToRegexp converts a tocall wildcard pattern to a regexp.
// Wildcards: n → \d, ? → ., * → .*
// The wildcard portion is captured in a group for version extraction.
// Literal characters after wildcards are placed outside the capture group.
func tocallToRegexp(pattern string) (*regexp.Regexp, error) {
	// Split into: prefix (literal), wild (wildcards), suffix (literal after wildcards)
	var prefix strings.Builder
	var wild strings.Builder
	var suffix strings.Builder
	phase := 0 // 0=prefix, 1=wild, 2=suffix

	for _, c := range pattern {
		isWild := c == 'n' || c == '?' || c == '*'
		switch {
		case phase == 0 && !isWild:
			prefix.WriteRune(c)
		case phase == 0 && isWild:
			phase = 1
			fallthrough
		case phase == 1 && isWild:
			switch c {
			case 'n':
				wild.WriteString(`\d`)
			case '?':
				wild.WriteByte('.')
			case '*':
				wild.WriteString(`.*`)
			}
		case phase == 1 && !isWild:
			phase = 2
			fallthrough
		case phase == 2:
			if isWild {
				// More wildcards after literal suffix - unusual, but handle
				switch c {
				case 'n':
					suffix.WriteString(`\d`)
				case '?':
					suffix.WriteByte('.')
				case '*':
					suffix.WriteString(`.*`)
				}
			} else {
				suffix.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
	}

	var reStr string
	if wild.Len() > 0 {
		reStr = `^` + regexp.QuoteMeta(prefix.String()) + `(` + wild.String() + `)` + suffix.String() + `$`
	} else {
		reStr = `^` + regexp.QuoteMeta(prefix.String()) + `$`
	}

	return regexp.Compile(reStr)
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
		fixedTocalls:  make(map[string]*tocallEntry),
		regexpTocalls: make(map[string][]*regexpTocall),
		miceCodes:     make(map[string]*miceEntry),
		miceLegacy:    yf.MiceLegacy,
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
			re, err := tocallToRegexp(t.Tocall)
			if err != nil {
				return nil, fmt.Errorf("compiling tocall pattern %q: %w", t.Tocall, err)
			}
			plen := prefixLen(t.Tocall)
			prefix := t.Tocall[:plen]
			db.regexpTocalls[prefix] = append(db.regexpTocalls[prefix], &regexpTocall{
				re:    re,
				entry: entry,
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

// Mic-E legacy device patterns
var (
	reLegacyD72  = regexp.MustCompile(`^>(.*?)=$`)
	reLegacyD74  = regexp.MustCompile(`^>(.*?)\^$`)
	reLegacyD7   = regexp.MustCompile(`^>`)
	reLegacyD710 = regexp.MustCompile(`^\](.*?)=$`)
	reLegacyD700 = regexp.MustCompile(`^\]`)
	reLegacyVX8  = regexp.MustCompile("^`.*_\\s*$")
	reNewMicE    = regexp.MustCompile("^([`'])(.*)(..)[|>]?$")
)

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
		if rts, ok := db.regexpTocalls[prefix]; ok {
			for _, rt := range rts {
				m := rt.re.FindStringSubmatch(dst)
				if m != nil {
					version := ""
					if len(m) > 1 {
						version = m[1]
					}
					return entryToDeviceID(rt.entry, version), comment, nil
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
