package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLlamaAssetURLCoversTheShippedPlatforms(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"darwin", "arm64", "llama-" + llamaNightlyTag + "-bin-macos-arm64.tar.gz"},
		{"darwin", "amd64", "llama-" + llamaNightlyTag + "-bin-macos-x64.tar.gz"},
		{"linux", "amd64", "llama-" + llamaNightlyTag + "-bin-ubuntu-x64.tar.gz"},
		{"linux", "arm64", "llama-" + llamaBuilderTag + "-bin-ubuntu-cpu-arm64.tar.gz"},
		{"windows", "amd64", "llama-" + llamaNightlyTag + "-bin-win-cpu-x64.zip"},
		{"windows", "arm64", "llama-" + llamaNightlyTag + "-bin-win-cpu-arm64.zip"},
	}
	for _, tc := range cases {
		got, err := llamaAssetURL(tc.goos, tc.goarch)
		if err != nil {
			t.Fatalf("llamaAssetURL(%s/%s): %v", tc.goos, tc.goarch, err)
		}
		if !strings.HasSuffix(got, "/"+tc.want) {
			t.Errorf("llamaAssetURL(%s/%s) = %q, want it to end in %q", tc.goos, tc.goarch, got, tc.want)
		}
		// Linux ARM64 is the one target ggml-org does not publish, so it has to
		// come from the llama-cpp-builder mirror instead.
		wantBase := llamaReleaseBase
		if tc.goos == "linux" && tc.goarch == "arm64" {
			wantBase = llamaBuilderBase
		}
		if !strings.HasPrefix(got, wantBase) {
			t.Errorf("llamaAssetURL(%s/%s) = %q, want it served from %q", tc.goos, tc.goarch, got, wantBase)
		}
	}
}

// A platform with no prebuilt CPU build must say so in a way the caller can tell
// apart from a failed download, because the fix is different.
func TestLlamaAssetURLRejectsAnUnsupportedPlatform(t *testing.T) {
	_, err := llamaAssetURL("plan9", "riscv64")
	if !errors.Is(err, errUnsupportedLocalPlatform) {
		t.Fatalf("error = %v, want errUnsupportedLocalPlatform", err)
	}
}

func TestIsSharedLibraryName(t *testing.T) {
	libraries := []string{"libllama.dylib", "libllama.0.4.1.dylib", "libggml.so", "libggml.so.0.24.0", "llama.dll", "GGML.DLL"}
	others := []string{"llama-cli", "llama-server.exe", "LICENSE", "libllama.dylib.txt", "readme.so.md.txt"}

	for _, name := range libraries {
		if !isSharedLibraryName(name) {
			t.Errorf("isSharedLibraryName(%q) = false, want true", name)
		}
	}
	for _, name := range others {
		if isSharedLibraryName(name) {
			t.Errorf("isSharedLibraryName(%q) = true, want false", name)
		}
	}
}

// The release archives carry the whole llama.cpp tool suite. Extracting the
// executables would multiply the install size for files purego never opens.
func TestExtractSharedLibrariesKeepsOnlyLibraries(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "llama.tar.gz")
	writeTestTarGz(t, archive, []tarEntry{
		{name: "llama-b1/libllama.0.4.1.dylib", body: "real library"},
		{name: "llama-b1/libllama.dylib", link: "libllama.0.4.1.dylib"},
		{name: "llama-b1/libggml-base.dylib", body: "ggml"},
		{name: "llama-b1/llama-cli", body: "an executable"},
		{name: "llama-b1/LICENSE", body: "text"},
	})

	dest := filepath.Join(t.TempDir(), "lib")
	written, err := extractSharedLibraries(archive, dest)
	if err != nil {
		t.Fatalf("extractSharedLibraries: %v", err)
	}
	if written != 3 {
		t.Fatalf("written = %d, want the three libraries", written)
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("read %s: %v", dest, err)
	}
	if len(entries) != 3 {
		t.Fatalf("extracted %d entries, want 3: %v", len(entries), entries)
	}

	// The link must stay a link: the versioned file is what the other libraries
	// record as their dependency, and a copy would load llama.cpp twice.
	info, err := os.Lstat(filepath.Join(dest, "libllama.dylib"))
	if err != nil {
		t.Fatalf("lstat libllama.dylib: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("libllama.dylib is a regular file, want a symlink")
	}
	body, err := os.ReadFile(filepath.Join(dest, "libllama.dylib"))
	if err != nil {
		t.Fatalf("read through the link: %v", err)
	}
	if string(body) != "real library" {
		t.Errorf("link resolves to %q, want the versioned library", body)
	}
}

// An archive is remote input. A link that points outside the install directory
// must be refused rather than written.
func TestExtractSharedLibrariesRefusesAnEscapingSymlink(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "evil.tar.gz")
	writeTestTarGz(t, archive, []tarEntry{
		{name: "llama-b1/libllama.dylib", link: "../../../../etc/passwd"},
	})

	dest := filepath.Join(t.TempDir(), "lib")
	if _, err := extractSharedLibraries(archive, dest); err == nil {
		t.Fatal("extractSharedLibraries accepted a link outside the directory")
	}
}

// An entry name reaches filepath.Join, and on Windows a backslash inside it is
// a separator — which path.Base does not know, because archive paths use forward
// slashes. A name that could leave the install directory is refused outright.
func TestLibraryEntryNameRefusesSeparators(t *testing.T) {
	for entry, want := range map[string]string{
		"llama-b1/libllama.dylib": "libllama.dylib",
		"libggml.so.0":            "libggml.so.0",
	} {
		got, ok := libraryEntryName(entry)
		if !ok || got != want {
			t.Errorf("libraryEntryName(%q) = %q, %v; want %q, true", entry, got, ok, want)
		}
	}
	for _, entry := range []string{
		`..\..\evil.dll`,
		`llama-b1/..\..\evil.dll`,
		`C:\Windows\System32\evil.dll`,
		"..",
	} {
		if got, ok := libraryEntryName(entry); ok {
			t.Errorf("libraryEntryName(%q) = %q, true; want it refused", entry, got)
		}
	}
}

// The same refusal has to hold through the unpacker, not only in the helper.
func TestExtractSharedLibrariesRefusesABackslashEntry(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "evil.tar.gz")
	writeTestTarGz(t, archive, []tarEntry{
		{name: `llama-b1/..\..\evil.dll`, body: "payload"},
		{name: "llama-b1/libllama.dylib", body: "real library"},
	})

	dest := filepath.Join(t.TempDir(), "lib")
	written, err := extractSharedLibraries(archive, dest)
	if err != nil {
		t.Fatalf("extractSharedLibraries: %v", err)
	}
	if written != 1 {
		t.Fatalf("written = %d, want only the real library", written)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "libllama.dylib" {
		t.Errorf("installed %v, want only libllama.dylib", entries)
	}
}

func TestExtractSharedLibrariesReadsZipArchives(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "llama.zip")
	writeTestZip(t, archive, map[string]string{
		"llama-b1/llama.dll":      "windows library",
		"llama-b1/ggml.dll":       "ggml",
		"llama-b1/llama-cli.exe":  "an executable",
		"llama-b1/llama-bench.md": "text",
	})

	dest := filepath.Join(t.TempDir(), "lib")
	written, err := extractSharedLibraries(archive, dest)
	if err != nil {
		t.Fatalf("extractSharedLibraries: %v", err)
	}
	if written != 2 {
		t.Fatalf("written = %d, want the two DLLs", written)
	}
	if body, err := os.ReadFile(filepath.Join(dest, "llama.dll")); err != nil || string(body) != "windows library" {
		t.Fatalf("llama.dll = %q, %v", body, err)
	}
}

// --- integrity ---

// What comes down is loaded into this process as native code. A manifest whose
// bytes do not match the value encli was built with must stop the install, not
// downgrade it to an unchecked one.
func TestLlamaDigestFromManifestFailsClosed(t *testing.T) {
	const assetURL = llamaReleaseBase + "/" + llamaNightlyTag + "/llama-" + llamaNightlyTag + "-bin-macos-arm64.tar.gz"
	body := []byte(`{"sources":{"ggml-org/llama.cpp":{"tag":"` + llamaNightlyTag +
		`","assets":{"llama-` + llamaNightlyTag + `-bin-macos-arm64.tar.gz":{"sha256":"abc123"}}}}}`)
	pin := fmt.Sprintf("%x", sha256.Sum256(body))

	got, err := llamaDigestFromManifest(body, pin, assetURL)
	if err != nil {
		t.Fatalf("llamaDigestFromManifest: %v", err)
	}
	if got != "abc123" {
		t.Errorf("digest = %q, want the value the manifest gives", got)
	}

	// A manifest someone swapped out.
	_, err = llamaDigestFromManifest(append(body, ' '), pin, assetURL)
	if !errors.Is(err, errDigestMismatch) {
		t.Errorf("a tampered manifest gave %v, want errDigestMismatch", err)
	}

	// A manifest that is authentic but says nothing about this asset. Falling
	// back to no check would make the pin decorative.
	for _, unknown := range []string{
		llamaReleaseBase + "/" + llamaNightlyTag + "/llama-" + llamaNightlyTag + "-bin-macos-x64.tar.gz",
		llamaBuilderBase + "/" + llamaBuilderTag + "/llama-" + llamaBuilderTag + "-bin-ubuntu-cpu-arm64.tar.gz",
		llamaReleaseBase + "/b00000/llama-b00000-bin-macos-arm64.tar.gz",
		"https://example.invalid/llama.tar.gz",
	} {
		if _, err := llamaDigestFromManifest(body, pin, unknown); err == nil {
			t.Errorf("llamaDigestFromManifest(%q) succeeded, want a refusal", unknown)
		}
	}
}

// The check has to happen before the file is moved into the cache, or a
// substituted archive would be unpacked and dlopened once and rejected forever
// after.
func TestDownloadFileRejectsAWrongDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not the archive you asked for"))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "llama.tar.gz")
	err := downloadFile(t.Context(), server.URL+"/llama.tar.gz", dest, strings.Repeat("0", 64), nil)
	if !errors.Is(err, errDigestMismatch) {
		t.Fatalf("error = %v, want errDigestMismatch", err)
	}
	if leftovers := downloadLeftovers(t, dest); len(leftovers) != 0 {
		t.Errorf("a failed check left %v behind", leftovers)
	}

	// The same body under its real digest installs.
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("not the archive you asked for")))
	if err := downloadFile(t.Context(), server.URL+"/llama.tar.gz", dest, want, nil); err != nil {
		t.Fatalf("downloadFile with the right digest: %v", err)
	}
}

// The default model is the one file encli fetches without being asked, so it is
// the one that carries a pinned digest.
func TestEnsureModelChecksTheDefaultModelDigest(t *testing.T) {
	t.Setenv(llmLocalDirEnvVar, t.TempDir())

	if len(defaultLocalModelSHA256) != 64 {
		t.Fatalf("defaultLocalModelSHA256 = %q, want a hex SHA-256", defaultLocalModelSHA256)
	}
	// Served from somewhere that is not the pinned URL, so the bytes must be
	// accepted: there is no digest on record for a model the operator chose.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("someone else's weights"))
	}))
	defer server.Close()

	manager := &localAssetManager{}
	if _, err := manager.ensureModel(t.Context(), server.URL+"/other.gguf"); err != nil {
		t.Fatalf("a model the operator named must not be checked: %v", err)
	}
}

// --- model download ---

// A first run downloads the weights and a second one finds them, because a
// 400 MB transfer per process start would make the local transport unusable.
func TestEnsureModelDownloadsOnceAndCaches(t *testing.T) {
	t.Setenv(llmLocalDirEnvVar, t.TempDir())

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte("GGUF weights"))
	}))
	defer server.Close()

	manager := &localAssetManager{}
	url := server.URL + "/models/test-model.gguf"

	var progress []string
	unsubscribe := manager.subscribe(func(line string) { progress = append(progress, line) })
	path, err := manager.ensureModel(t.Context(), url)
	if err != nil {
		t.Fatalf("ensureModel: %v", err)
	}
	if want := filepath.Join(localModelsDir(), "test-model.gguf"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "GGUF weights" {
		t.Fatalf("cached file = %q, %v", body, err)
	}

	if len(progress) == 0 {
		t.Error("the download reported nothing; a silent 400 MB transfer reads as a hang")
	}

	progress = nil
	again, err := manager.ensureModel(t.Context(), url)
	unsubscribe()
	if err != nil {
		t.Fatalf("second ensureModel: %v", err)
	}
	if again != path {
		t.Errorf("second path = %q, want %q", again, path)
	}
	if requests.Load() != 1 {
		t.Errorf("server saw %d requests, want the download to happen once", requests.Load())
	}
	if len(progress) != 0 {
		t.Errorf("a cached model reported %v, want silence", progress)
	}
}

// An interrupted transfer must not leave a truncated GGUF behind: every later
// run would take it for the cached model and fail to load it.
func TestDownloadFileLeavesNothingBehindOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.Write([]byte("truncated"))
		w.(http.Flusher).Flush()
		// Closing without the promised bytes is what a dropped connection does.
		if hijacker, ok := w.(http.Hijacker); ok {
			conn, _, err := hijacker.Hijack()
			if err == nil {
				conn.Close()
			}
		}
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "model.gguf")
	if err := downloadFile(t.Context(), server.URL+"/model.gguf", dest, "", nil); err == nil {
		t.Fatal("downloadFile reported success on a truncated body")
	}
	if leftovers := downloadLeftovers(t, dest); len(leftovers) != 0 {
		t.Errorf("the failure left %v behind", leftovers)
	}
}

// A non-HTTP reference is a file the operator already has. Fetching it is not an
// option, so a missing one is an error rather than a download.
func TestEnsureModelUsesAConfiguredPath(t *testing.T) {
	t.Setenv(llmLocalDirEnvVar, t.TempDir())
	manager := &localAssetManager{}

	existing := filepath.Join(t.TempDir(), "own.gguf")
	if err := os.WriteFile(existing, []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := manager.ensureModel(t.Context(), existing)
	if err != nil {
		t.Fatalf("ensureModel: %v", err)
	}
	if got != existing {
		t.Errorf("path = %q, want %q", got, existing)
	}

	if _, err := manager.ensureModel(t.Context(), filepath.Join(t.TempDir(), "absent.gguf")); err == nil {
		t.Error("ensureModel accepted a path that does not exist")
	}
}

func TestModelFileNameFromURL(t *testing.T) {
	name, err := modelFileNameFromURL(defaultLocalModelURL)
	if err != nil {
		t.Fatalf("modelFileNameFromURL: %v", err)
	}
	if name != "Qwen_Qwen3-VL-2B-Instruct-Q4_K_M.gguf" {
		t.Errorf("name = %q", name)
	}
	if _, err := modelFileNameFromURL("https://example.invalid/"); err == nil {
		t.Error("a URL with no file name was accepted")
	}
}

// --- libraries ---

// A configured directory is someone's own llama.cpp build. Unpacking a CPU
// build on top of it would replace a CUDA install the operator chose.
func TestEnsureLibrariesNeverWritesIntoAConfiguredDirectory(t *testing.T) {
	manager := &localAssetManager{}
	empty := t.TempDir()

	_, err := manager.ensureLibraries(t.Context(), empty)
	if err == nil {
		t.Fatal("ensureLibraries accepted a directory with no llama.cpp in it")
	}
	if !strings.Contains(err.Error(), "LLM_LOCAL_LIB") {
		t.Errorf("error = %v, want it to name the variable that points here", err)
	}
	entries, _ := os.ReadDir(empty)
	if len(entries) != 0 {
		t.Errorf("the directory now holds %v, want it untouched", entries)
	}

	// With the library in place the same directory is accepted as it is.
	marker := filepath.Join(empty, localLibraryFileName(runtime.GOOS))
	if err := os.WriteFile(marker, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := manager.ensureLibraries(t.Context(), empty)
	if err != nil {
		t.Fatalf("ensureLibraries: %v", err)
	}
	if got != empty {
		t.Errorf("path = %q, want %q", got, empty)
	}
}

// The managed install happens once. A second run must find it rather than
// download and unpack 30 MB of libraries again on every process start.
func TestEnsureLibrariesSkipsAnInstallThatIsAlreadyThere(t *testing.T) {
	t.Setenv(llmLocalDirEnvVar, t.TempDir())
	manager := &localAssetManager{}

	if err := os.MkdirAll(localLibDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(localLibDir(), localLibraryFileName(runtime.GOOS))
	if err := os.WriteFile(marker, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}

	// No network is reachable from here, so a download would fail rather than
	// quietly succeed: reaching the managed directory is the whole assertion.
	got, err := manager.ensureLibraries(t.Context(), "")
	if err != nil {
		t.Fatalf("ensureLibraries: %v", err)
	}
	if got != localLibDir() {
		t.Errorf("path = %q, want %q", got, localLibDir())
	}
	body, err := os.ReadFile(marker)
	if err != nil || string(body) != "stub" {
		t.Errorf("the existing install was overwritten: %q, %v", body, err)
	}
}

// The scratch names carry a pid so two processes cannot collide, which means
// nothing ever reuses them. With -web starting a 400 MB download at launch, a
// Ctrl-C leaves hundreds of megabytes that no later run would look at.
func TestSweepStaleLocalScratchRemovesAbandonedWork(t *testing.T) {
	t.Setenv(llmLocalDirEnvVar, t.TempDir())
	if err := os.MkdirAll(localModelsDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-2 * localScratchMaxAge)
	stale := []string{
		filepath.Join(localModelsDir(), "model.gguf.part-999"),
		filepath.Join(localCacheDir(), "999-llama-b1-bin-macos-arm64.tar.gz"),
	}
	for _, path := range stale {
		if err := os.WriteFile(path, []byte("half a download"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	staleDir := localLibDir() + ".incomplete-999"
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(staleDir, old, old); err != nil {
		t.Fatal(err)
	}

	// A download another process still has in flight keeps being written, so it
	// stays young — and must survive.
	live := filepath.Join(localModelsDir(), "model.gguf.part-1000")
	if err := os.WriteFile(live, []byte("in flight"), 0o600); err != nil {
		t.Fatal(err)
	}
	// So must the cache the scratch sits beside.
	cached := filepath.Join(localModelsDir(), "model.gguf")
	if err := os.WriteFile(cached, []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(cached, old, old); err != nil {
		t.Fatal(err)
	}

	sweepStaleLocalScratch()

	for _, path := range append(stale, staleDir) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s survived the sweep", path)
		}
	}
	for _, path := range []string{live, cached} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was swept away: %v", path, err)
		}
	}
}

// Following the redirect is not optional — GitHub and Hugging Face both use them
// — but dropping TLS on the way is, and the model an operator names has no
// digest to fall back on.
func TestRefuseDowngradeToHTTP(t *testing.T) {
	secure := httptest.NewRequest(http.MethodGet, "https://example.test/model.gguf", nil)

	if err := refuseDowngradeToHTTP(
		httptest.NewRequest(http.MethodGet, "https://cdn.example.test/model.gguf", nil),
		[]*http.Request{secure},
	); err != nil {
		t.Errorf("an https redirect was refused: %v", err)
	}

	err := refuseDowngradeToHTTP(
		httptest.NewRequest(http.MethodGet, "http://cdn.example.test/model.gguf", nil),
		[]*http.Request{secure},
	)
	if err == nil {
		t.Fatal("a redirect from https to http was followed")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("error = %v, want it to name the downgrade", err)
	}

	// A chain that was plain http to begin with is the operator's own choice and
	// is left alone; the limit still applies.
	insecure := httptest.NewRequest(http.MethodGet, "http://example.test/model.gguf", nil)
	if err := refuseDowngradeToHTTP(insecure, []*http.Request{insecure}); err != nil {
		t.Errorf("an http chain was refused: %v", err)
	}
	chain := make([]*http.Request, maxLocalRedirects)
	for i := range chain {
		chain[i] = secure
	}
	if err := refuseDowngradeToHTTP(secure, chain); err == nil {
		t.Error("an endless redirect chain was followed")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0: "0 B", 512: "512 B", 2048: "2.0 KiB", 5 << 20: "5.0 MiB", 3 << 30: "3.0 GiB",
		// A Content-Length is whatever a server chose to send. Running off the
		// end of the unit table would panic on a progress line.
		1 << 50: "1.0 PiB",
		1 << 62: "4096.0 PiB",
	}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDownloadProgressText(t *testing.T) {
	if got := downloadProgressText(512<<20, 1<<30); got != "512.0 MiB / 1.0 GiB (50%)" {
		t.Errorf("progress = %q", got)
	}
	// A server that sends no Content-Length leaves the share unknowable; a made
	// up percentage would be worse than none.
	if got := downloadProgressText(1024, 0); got != "1.0 KiB" {
		t.Errorf("progress without a total = %q", got)
	}
}

// downloadLeftovers lists whatever a download put beside dest, including the
// per-process scratch file, so a test can assert that a failure left nothing.
func downloadLeftovers(t *testing.T, dest string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Dir(dest), err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), filepath.Base(dest)) {
			names = append(names, entry.Name())
		}
	}
	return names
}

// --- helpers ---

type tarEntry struct {
	name string
	body string
	link string
}

func writeTestTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o755}
		if entry.link != "" {
			header.Typeflag = tar.TypeSymlink
			header.Linkname = entry.link
		} else {
			header.Typeflag = tar.TypeReg
			header.Size = int64(len(entry.body))
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.link == "" {
			if _, err := tw.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
