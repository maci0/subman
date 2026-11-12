package update

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func assets() []Asset {
	return []Asset{
		{Name: "subman-linux-amd64", URL: "https://github.com/maci0/subman/releases/download/v0.2.0/subman-linux-amd64"},
		{Name: "SHA256SUMS", URL: "https://github.com/maci0/subman/releases/download/v0.2.0/SHA256SUMS"},
	}
}

func TestAssetName(t *testing.T) {
	got := map[string]string{
		"linux/amd64":   AssetName("linux", "amd64"),
		"linux/arm64":   AssetName("linux", "arm64"),
		"darwin/amd64":  AssetName("darwin", "amd64"),
		"darwin/arm64":  AssetName("darwin", "arm64"),
		"windows/amd64": AssetName("windows", "amd64"),
		"windows/arm64": AssetName("windows", "arm64"),
		"freebsd/amd64": AssetName("freebsd", "amd64"),
	}
	want := map[string]string{
		"linux/amd64":   "subman-linux-amd64",
		"linux/arm64":   "subman-linux-arm64",
		"darwin/amd64":  "subman-darwin-amd64",
		"darwin/arm64":  "subman-darwin-arm64",
		"windows/amd64": "subman-windows-amd64.exe",
		"windows/arm64": "subman-windows-arm64.exe",
		"freebsd/amd64": "",
	}
	for key, name := range want {
		if got[key] != name {
			t.Errorf("%s: got %q", key, got[key])
		}
	}
}

func TestCheckAvailable(t *testing.T) {
	if runtime.GOARCH != "amd64" || runtime.GOOS != "linux" {
		t.Skip("asset name is host-specific")
	}
	p := Check("0.1.0", "v0.2.0", assets())
	if !p.Status.Available || p.Asset.Name != "subman-linux-amd64" || p.Sums.Name != "SHA256SUMS" {
		t.Fatalf("%+v", p)
	}
}

func TestCheckCurrentAndAhead(t *testing.T) {
	p := Check("0.2.0", "v0.2.0", assets())
	if p.Status.Available || p.Status.Ahead {
		t.Fatalf("%+v", p.Status)
	}
	p = Check("0.3.0", "v0.2.0", assets())
	if !p.Status.Ahead || p.Status.Available {
		t.Fatalf("%+v", p.Status)
	}
}

func TestCheckRejectsMissingSums(t *testing.T) {
	if runtime.GOARCH != "amd64" || runtime.GOOS != "linux" {
		t.Skip()
	}
	only := []Asset{{Name: "subman-linux-amd64", URL: "https://github.com/maci0/subman/releases/download/v0.2.0/subman-linux-amd64"}}
	p := Check("0.1.0", "v0.2.0", only)
	if p.Status.Available {
		t.Fatal(p.Status)
	}
}

func TestSumFor(t *testing.T) {
	line := "deafbeefdeafbeefdeafbeefdeafbeefdeafbeefdeafbeefdeafbeefdeafbeef  subman-linux-amd64\n"
	got, ok := sumFor(line, "subman-linux-amd64")
	if !ok || len(got) != 64 {
		t.Fatal(got, ok)
	}
	if _, ok := sumFor("aa  other\n", "subman-linux-amd64"); ok {
		t.Fatal("matched a different name")
	}
	if _, ok := sumFor("zz  subman-linux-amd64\n", "subman-linux-amd64"); ok {
		t.Fatal("accepted a non-hex digest")
	}
}

func TestTrustedAndRepo(t *testing.T) {
	if trustedURL("http://github.com/x") || trustedURL("https://example.com/x") {
		t.Fatal("untrusted accepted")
	}
	if !trustedURL("https://github.com/maci0/subman/releases/download/v0.2.0/a") {
		t.Fatal("github rejected")
	}
	if validRepo("../x") || validRepo("a/b/c") || validRepo(".hidden/subman") || validRepo("maci0/-subman") || !validRepo("maci0/subman") {
		t.Fatal("repo check")
	}
}

func TestInstallRefusesWhenCurrent(t *testing.T) {
	p := Check("1.0.0", "v1.0.0", nil)
	if err := Install(t.Context(), p, t.TempDir()+"/subman"); err == nil {
		t.Fatal("installed a current build")
	}
}

func TestDownloadHonorsCap(t *testing.T) {
	blob := []byte("hello subman")
	sum := sha256.Sum256(blob)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			w.Write(blob)
		case "/s":
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  subman-linux-amd64\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := download(t.Context(), srv.URL+"/a", int64(len(blob)))
	if err != nil || string(got) != string(blob) {
		t.Fatal(err, string(got))
	}
	if _, err := download(t.Context(), srv.URL+"/a", 4); err == nil {
		t.Fatal("cap not enforced")
	}
	// Install itself refuses a non-GitHub URL even when the digest matches.
	plan := Plan{
		Status:   Check("0.1.0", "v0.2.0", assets()).Status,
		WantName: "subman-linux-amd64",
		Asset:    Asset{URL: srv.URL + "/a"},
		Sums:     Asset{URL: srv.URL + "/s"},
	}
	plan.Status.Available = true
	if err := Install(t.Context(), plan, t.TempDir()+"/subman"); err == nil {
		t.Fatal("installed from a non-github url")
	}
}
