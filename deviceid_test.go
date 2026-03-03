package deviceid

import (
	"errors"
	"os"
	"testing"

	"github.com/hessu/go-aprs-fap"
)

const testDBPath = "testdata/tocalls.yaml"

func openTestDB(t *testing.T) *DeviceDB {
	t.Helper()
	ddb, err := Open(testDBPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	return ddb
}

func TestExactTocallMatch(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		DstCallsign: "APZMDR",
		Format:      fap.FormatUncompressed,
	}

	did, _, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Vendor != "Open Source" {
		t.Errorf("vendor = %q, want %q", did.Vendor, "Open Source")
	}
	if did.Model != "HaMDR" {
		t.Errorf("model = %q, want %q", did.Model, "HaMDR")
	}
	if did.Class != "tracker" {
		t.Errorf("class = %q, want %q", did.Class, "tracker")
	}
	if did.OS != "embedded" {
		t.Errorf("os = %q, want %q", did.OS, "embedded")
	}
}

func TestWildcardTocallMatch(t *testing.T) {
	ddb := openTestDB(t)

	tests := []struct {
		name   string
		tocall string
		vendor string
		model  string
	}{
		{"APDnnn", "APD123", "Open Source", "aprsd"},
		{"APDR??", "APDR15", "Open Source", "APRSdroid"},
		{"APnnnU", "AP123U", "Painter Engineering", "uSmartDigi Digipeater"},
		{"APWW??", "APWW10", "KJ4ERJ", "APRSIS32"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &fap.Packet{
				DstCallsign: tc.tocall,
				Format:      fap.FormatUncompressed,
			}

			did, _, err := ddb.Identify(p)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if did.Vendor != tc.vendor {
				t.Errorf("vendor = %q, want %q", did.Vendor, tc.vendor)
			}
			if did.Model != tc.model {
				t.Errorf("model = %q, want %q", did.Model, tc.model)
			}
		})
	}
}

func TestWildcardWithFeatures(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		DstCallsign: "APWW10",
		Format:      fap.FormatUncompressed,
	}

	did, _, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !did.Messaging {
		t.Error("expected Messaging to be true")
	}
	if len(did.Features) != 2 {
		t.Errorf("features = %v, want 2 features", did.Features)
	}
}

func TestMicELegacyKenwoodD7(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		Format:      fap.FormatMicE,
		DstCallsign: "DUMMY",
		Comment:     ">hello",
	}

	did, comment, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Vendor != "Kenwood" {
		t.Errorf("vendor = %q, want %q", did.Vendor, "Kenwood")
	}
	if did.Model != "TH-D7A" {
		t.Errorf("model = %q, want %q", did.Model, "TH-D7A")
	}
	if did.Class != "ht" {
		t.Errorf("class = %q, want %q", did.Class, "ht")
	}
	if !did.Messaging {
		t.Error("expected Messaging to be true")
	}
	if comment != "hello" {
		t.Errorf("comment = %q, want %q", comment, "hello")
	}
}

func TestMicELegacyKenwoodD72(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		Format:      fap.FormatMicE,
		DstCallsign: "DUMMY",
		Comment:     ">some text=",
	}

	did, comment, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Model != "TH-D72" {
		t.Errorf("model = %q, want %q", did.Model, "TH-D72")
	}
	if comment != "some text" {
		t.Errorf("comment = %q, want %q", comment, "some text")
	}
}

func TestMicELegacyKenwoodD74(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		Format:      fap.FormatMicE,
		DstCallsign: "DUMMY",
		Comment:     ">some text^",
	}

	did, comment, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Model != "TH-D74" {
		t.Errorf("model = %q, want %q", did.Model, "TH-D74")
	}
	if comment != "some text" {
		t.Errorf("comment = %q, want %q", comment, "some text")
	}
}

func TestMicELegacyKenwoodD700(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		Format:      fap.FormatMicE,
		DstCallsign: "DUMMY",
		Comment:     "]hello world",
	}

	did, comment, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Model != "TM-D700" {
		t.Errorf("model = %q, want %q", did.Model, "TM-D700")
	}
	if comment != "hello world" {
		t.Errorf("comment = %q, want %q", comment, "hello world")
	}
}

func TestMicELegacyKenwoodD710(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		Format:      fap.FormatMicE,
		DstCallsign: "DUMMY",
		Comment:     "]some text=",
	}

	did, comment, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Model != "TM-D710" {
		t.Errorf("model = %q, want %q", did.Model, "TM-D710")
	}
	if comment != "some text" {
		t.Errorf("comment = %q, want %q", comment, "some text")
	}
}

func TestMicENewStyleSuffix(t *testing.T) {
	ddb := openTestDB(t)

	tests := []struct {
		name      string
		comment   string
		vendor    string
		model     string
		messaging bool
	}{
		{"Byonics TinyTrak3 with backtick", "`test data|3", "Byonics", "TinyTrak3", true},
		{"Byonics TinyTrak4 with quote", "'test data|4", "Byonics", "TinyTrak4", false},
		{"Yaesu VX-8", "`data_ ", "Yaesu", "VX-8", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &fap.Packet{
				Format:      fap.FormatMicE,
				DstCallsign: "DUMMY",
				Comment:     tc.comment,
			}

			did, _, err := ddb.Identify(p)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if did.Vendor != tc.vendor {
				t.Errorf("vendor = %q, want %q", did.Vendor, tc.vendor)
			}
			if did.Model != tc.model {
				t.Errorf("model = %q, want %q", did.Model, tc.model)
			}
			if did.Messaging != tc.messaging {
				t.Errorf("messaging = %v, want %v", did.Messaging, tc.messaging)
			}
		})
	}
}

func TestMicECommentStripping(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		Format:      fap.FormatMicE,
		DstCallsign: "DUMMY",
		Comment:     "`hello world|4",
	}

	did, comment, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Model != "TinyTrak4" {
		t.Errorf("model = %q, want %q", did.Model, "TinyTrak4")
	}
	if comment != "`hello world" {
		t.Errorf("stripped comment = %q, want %q", comment, "`hello world")
	}
}

func TestNoMatch(t *testing.T) {
	ddb := openTestDB(t)

	p := &fap.Packet{
		DstCallsign: "ZZZZZ9",
		Format:      fap.FormatUncompressed,
	}

	did, _, err := ddb.Identify(p)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("expected ErrNoMatch, got %v", err)
	}
	if did != nil {
		t.Errorf("expected nil DeviceID, got %+v", did)
	}
}

func TestMicEFallbackToTocall(t *testing.T) {
	ddb := openTestDB(t)

	// Mic-E format but comment doesn't match any Mic-E device code;
	// should fall back to tocall matching
	p := &fap.Packet{
		Format:      fap.FormatMicE,
		DstCallsign: "APZMDR",
		Comment:     "xy",
	}

	did, _, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Model != "HaMDR" {
		t.Errorf("model = %q, want %q", did.Model, "HaMDR")
	}
}

func TestOpenFileNotFound(t *testing.T) {
	_, err := Open("nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCheckReloadNoChange(t *testing.T) {
	ddb := openTestDB(t)

	reloaded, err := ddb.CheckReload()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reloaded {
		t.Error("expected no reload when file unchanged")
	}
}

func TestCheckReloadAfterChange(t *testing.T) {
	// Copy testdata to a temp file so we can modify it
	src, err := os.ReadFile(testDBPath)
	if err != nil {
		t.Fatalf("reading test db: %v", err)
	}

	tmp, err := os.CreateTemp(t.TempDir(), "tocalls-*.yaml")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if _, err := tmp.Write(src); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	tmp.Close()

	ddb, err := Open(tmp.Name())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Verify initial load works
	p := &fap.Packet{DstCallsign: "APZMDR", Format: fap.FormatUncompressed}
	did, _, err := ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if did.Model != "HaMDR" {
		t.Fatalf("model = %q, want %q", did.Model, "HaMDR")
	}

	// Rewrite the file (new inode via rename) with modified content
	modified := []byte(`tocalls:
 - tocall: APZMDR
   vendor: Test Vendor
   model: Test Model
   class: tracker
`)
	tmp2, err := os.CreateTemp(t.TempDir(), "tocalls-new-*.yaml")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if _, err := tmp2.Write(modified); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	tmp2.Close()
	os.Rename(tmp2.Name(), tmp.Name())

	reloaded, err := ddb.CheckReload()
	if err != nil {
		t.Fatalf("CheckReload error: %v", err)
	}
	if !reloaded {
		t.Error("expected reload after file change")
	}

	// Verify new data is active
	did, _, err = ddb.Identify(p)
	if err != nil {
		t.Fatalf("unexpected error after reload: %v", err)
	}
	if did.Model != "Test Model" {
		t.Errorf("model after reload = %q, want %q", did.Model, "Test Model")
	}
}
