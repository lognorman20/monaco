package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveIOSSim_picksBootedIPhoneWhenSlimUnset(t *testing.T) {
	root := repoRoot(t)
	fakebin := filepath.Join(root, "scripts", "testdata", "fakebin")
	fixture := filepath.Join(root, "scripts", "testdata", "simctl")

	cmd := exec.Command("bash", filepath.Join(root, "scripts", "resolve-ios-sim.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+fakebin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_SIMCTL_DIR="+fixture,
		"SIMSLIM_UDID=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve-ios-sim.sh: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	// CombinedOutput includes stderr warnings. Last non-empty line is the UDID.
	lines := strings.Split(got, "\n")
	udid := strings.TrimSpace(lines[len(lines)-1])
	if udid != "BBBBBBBB-BBBB-BBBB-BBBB-BBBBBBBBBBBB" {
		t.Fatalf("expected booted iPhone UDID, got %q\nfull output:\n%s", udid, out)
	}
	if !strings.Contains(string(out), "SimSlim not installed") && !strings.Contains(string(out), "SIMSLIM_UDID unset") {
		t.Fatalf("expected a fallback warning, got:\n%s", out)
	}
}

// A fresh CI runner has several runtimes and nothing booted; the oldest can sit below the
// app's deployment target, so the pick must come from the newest runtime.
func TestResolveIOSSim_prefersNewestRuntimeWhenNothingBooted(t *testing.T) {
	root := repoRoot(t)
	fakebin := filepath.Join(root, "scripts", "testdata", "fakebin")
	fixture := filepath.Join(root, "scripts", "testdata", "simctl-shutdown")

	cmd := exec.Command("bash", filepath.Join(root, "scripts", "resolve-ios-sim.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+fakebin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_SIMCTL_DIR="+fixture,
		"SIMSLIM_UDID=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve-ios-sim.sh: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	udid := strings.TrimSpace(lines[len(lines)-1])
	if udid != "DDDDDDDD-DDDD-DDDD-DDDD-DDDDDDDDDDDD" {
		t.Fatalf("expected the iOS 26.0 iPhone, got %q\n%s", udid, out)
	}
}

func TestResolveIOSSim_usesSlimUdidWhenDeviceExists(t *testing.T) {
	root := repoRoot(t)
	fakebin := filepath.Join(root, "scripts", "testdata", "fakebin")
	fixture := filepath.Join(root, "scripts", "testdata", "simctl")

	cmd := exec.Command("bash", filepath.Join(root, "scripts", "resolve-ios-sim.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+fakebin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_SIMCTL_DIR="+fixture,
		"SIMSLIM_UDID=AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve-ios-sim.sh: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	udid := strings.TrimSpace(lines[len(lines)-1])
	if udid != "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA" {
		t.Fatalf("expected SIMSLIM_UDID, got %q\n%s", udid, out)
	}
	if !strings.Contains(string(out), "not installed or not ready") {
		t.Fatalf("expected slim-not-ready warning, got:\n%s", out)
	}
}

func TestResolveIOSSim_noCreateWhenEmpty(t *testing.T) {
	root := repoRoot(t)
	fakebin := filepath.Join(root, "scripts", "testdata", "fakebin")
	fixture := filepath.Join(root, "scripts", "testdata", "simctl-empty")

	cmd := exec.Command("bash", filepath.Join(root, "scripts", "resolve-ios-sim.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+fakebin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_SIMCTL_DIR="+fixture,
		"SIMSLIM_UDID=",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure with no simulators, got:\n%s", out)
	}
	if !strings.Contains(string(out), "no iOS Simulator available") {
		t.Fatalf("expected no-sim error, got:\n%s", out)
	}
}

func TestGoldSimUdid_failsWhenUnset(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "gold-sim-udid.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SIMSLIM_UDID=")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected fail when SIMSLIM_UDID unset, got:\n%s", out)
	}
	if !strings.Contains(string(out), "SIMSLIM_UDID is unset") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestGoldSimUdid_printsWhenDeviceExists(t *testing.T) {
	root := repoRoot(t)
	fakebin := filepath.Join(root, "scripts", "testdata", "fakebin")
	fixture := filepath.Join(root, "scripts", "testdata", "simctl")

	cmd := exec.Command("bash", filepath.Join(root, "scripts", "gold-sim-udid.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+fakebin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_SIMCTL_DIR="+fixture,
		"SIMSLIM_UDID=AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gold-sim-udid.sh: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA" {
		t.Fatalf("got %q\n%s", got, out)
	}
}
