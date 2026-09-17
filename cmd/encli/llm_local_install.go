package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// This file acquires the two things local inference needs on a machine that has
// never run it: the llama.cpp shared libraries that yzma dlopens, and the GGUF
// model weights. Both are fetched once and cached; every later run finds them.
//
// yzma ships its own installer in pkg/download, and it is deliberately not used.
// That package pulls in hashicorp/go-getter, which pulls in the AWS S3 SDK, the
// Google Cloud storage client and gRPC: linking it grew a probe binary from 2.9
// MB to 57.8 MB. encli would have paid ~55 MB for a URL table and an unpacker.
// What is reimplemented here is the subset that matters — the CPU/Metal builds
// for the platforms encli ships on — in terms of net/http and the archive
// packages in the standard library.

const (
	// defaultLocalModelURL is the model an operator gets without choosing one: a
	// 0.5B instruct model small enough (~400 MB) to download on first use and to
	// run on a laptop CPU, and one whose chat template declares tool calling,
	// which the agent depends on entirely.
	defaultLocalModelURL = "https://huggingface.co/bartowski/Qwen_Qwen3-VL-2B-Instruct-GGUF/resolve/main/Qwen_Qwen3-VL-2B-Instruct-Q4_K_M.gguf"

	// defaultLocalModelSHA256 is what that URL served when this was written.
	// Hugging Face lets a repository owner replace a file in place, and llama.cpp
	// parses a GGUF in C — so the one model encli fetches without being asked is
	// checked against a value that lives here rather than on the server.
	//
	// What it covers is the download. A file already sitting in the cache is not
	// re-hashed on every start, so anything able to write into that directory can
	// still put a GGUF there — but such a thing could equally replace the
	// libraries beside it, which are dlopened. The cache is a trusted location;
	// the network is not, and that is the line this draws.
	//
	// A model an operator names themselves is not checked: there is no digest to
	// check it against, and their URL is their decision. It is cached in a
	// different directory so it cannot take the built-in model's place — see
	// localPinnedModelsDir.
	defaultLocalModelSHA256 = "6eb923e7d26e9cea28811e1a8e852009b21242fb157b26149d3b188f3a8c8653"

	// llamaNightlyTag and llamaBuilderTag pin the llama.cpp build that is
	// installed. The nightly tag is where ggml-org publishes the binaries;
	// llama-cpp-builder republishes the same release under its own tag and is the
	// only source for the Linux ARM64 CPU build. Both are bumped together with
	// the yzma dependency — yzma v1.27.0 installs llama-cpp-builder v0.4.1, whose
	// nightly-tag.txt reads b10964.
	llamaNightlyTag = "b10964"
	llamaBuilderTag = "v0.4.1"

	// llamaReleaseBase and llamaBuilderBase are the two release pages the assets
	// come from.
	llamaReleaseBase = "https://github.com/ggml-org/llama.cpp/releases/download"
	llamaBuilderBase = "https://github.com/hybridgroup/llama-cpp-builder/releases/download"

	// llamaManifestSHA256 pins the digest manifest llama-cpp-builder publishes
	// for llamaBuilderTag. It is the root of the only integrity check there is on
	// the native code encli is about to dlopen: the pin authenticates the
	// manifest, and the manifest carries the digest of every release archive,
	// including the ones ggml-org serves.
	//
	// Pinning it here rather than trusting the manifest as served is the whole
	// point. Anyone who can replace an archive on the release page can replace
	// the manifest beside it, so a digest fetched from the same host catches a
	// damaged download and nothing else. This value is bumped together with
	// llamaBuilderTag; yzma v1.27.0 records the same one in its DefaultVersion.
	llamaManifestSHA256 = "e5fd75ea7d0f8f882de6f49968fb54fe19c4208882ae63b4d7eba97e9b747577"

	// llamaManifestLimit caps the manifest read. The published file is ~70 KB.
	llamaManifestLimit = 8 << 20

	// maxLocalDownloadBytes bounds what a download may write. It is generous —
	// the largest GGUF anyone would run on a laptop is far below it — and exists
	// so a server that answers with an endless body fills a bounded amount of
	// disk instead of all of it.
	maxLocalDownloadBytes = 32 << 30
)

// llmLocalDirEnvVar relocates the whole local-inference cache — libraries and
// models both. Tests use it to stay out of the developer's real home directory,
// and an operator with a small home partition can point it at a bigger disk.
const llmLocalDirEnvVar = "ENCLI_LLM_LOCAL_DIR"

// localCacheDir is where the libraries and the weights live. It is a sibling of
// the other encli state rather than a subdirectory of it because these are large
// binary artefacts, not settings: see stateFilePath for why nothing may be
// written directly into sessionDir().
func localCacheDir() string {
	if dir := strings.TrimSpace(os.Getenv(llmLocalDirEnvVar)); dir != "" {
		return dir
	}
	return filepath.Join(sessionDir(), "local-llm")
}

func localModelsDir() string { return filepath.Join(localCacheDir(), "models") }
func localLibDir() string    { return filepath.Join(localCacheDir(), "lib") }

// localPinnedModelsDir holds the one model encli downloads without being asked.
//
// It is a directory of its own so that a model an operator named themselves can
// never land on top of it. The cache key is the file name from the URL, and
// "Qwen_Qwen3-VL-2B-Instruct-Q4_K_M.gguf" is a name anyone can serve: fetched once
// from somewhere else, it would sit in the cache under exactly the name the
// built-in URL resolves to, and every later run would find it and skip the
// download — so defaultLocalModelSHA256 would never be applied again. Separate
// directories make that collision impossible rather than unlikely.
func localPinnedModelsDir() string { return filepath.Join(localModelsDir(), "builtin") }

// localModelRef is where a configured model reference points, and what its
// contents have to match once they get there.
type localModelRef struct {
	// path is the file to load: the operator's own, or where the download is
	// cached.
	path string
	// url is empty for a file the operator already has, and the source to fetch
	// from otherwise.
	url string
	// wantSHA256 is set only for the built-in model; see defaultLocalModelSHA256.
	wantSHA256 string
}

// resolveLocalModelRef reads a configured model reference. An empty one is the
// built-in model.
//
// Both callers that need this — the one that downloads and the one that only
// reports — go through here, because when they each had their own copy the two
// disagreed about what counts as present on disk.
func resolveLocalModelRef(ref string) (localModelRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = defaultLocalModelURL
	}
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		return localModelRef{path: expandLocalPath(ref)}, nil
	}
	name, err := modelFileNameFromURL(ref)
	if err != nil {
		return localModelRef{}, err
	}
	if ref == defaultLocalModelURL {
		return localModelRef{
			path:       filepath.Join(localPinnedModelsDir(), name),
			url:        ref,
			wantSHA256: defaultLocalModelSHA256,
		}, nil
	}
	return localModelRef{path: filepath.Join(localModelsDir(), name), url: ref}, nil
}

// present reports whether there is a file there to load.
//
// IsRegular, not merely a size: models/builtin is a directory, and a URL ending
// in "builtin" resolves to that very path. A non-empty directory would pass a
// size check and be handed to the GGUF parser as a model.
func (r localModelRef) present() bool {
	info, err := os.Stat(r.path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// name is what the progress lines call the model.
func (r localModelRef) name() string { return filepath.Base(r.path) }

// --- asset resolution ---

// errUnsupportedLocalPlatform is returned for a machine with no prebuilt CPU
// build. It is a distinct error so the caller can say "install the libraries
// yourself and set LLM_LOCAL_LIB" rather than "the download failed".
var errUnsupportedLocalPlatform = errors.New("no prebuilt llama.cpp libraries for this platform")

// errDigestMismatch marks a download whose bytes did not match the digest they
// were checked against, so a caller can say what to do about it.
var errDigestMismatch = errors.New("the downloaded file does not match its expected digest")

// llamaAssetURL reports the archive holding the llama.cpp shared libraries for
// one platform.
//
// Only the CPU builds (and Metal, which is what the macOS CPU build carries) are
// resolved here. A CUDA, ROCm or Vulkan build is a deliberate choice about a
// specific machine, and guessing it wrong installs gigabytes that cannot run;
// an operator who wants one installs it and points LLM_LOCAL_LIB at it.
func llamaAssetURL(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		// The macOS build carries libggml-metal, so this is the GPU build too.
		switch goarch {
		case "arm64":
			return llamaReleaseURL("llama-%s-bin-macos-arm64.tar.gz"), nil
		case "amd64":
			return llamaReleaseURL("llama-%s-bin-macos-x64.tar.gz"), nil
		}
	case "linux":
		switch goarch {
		case "amd64":
			return llamaReleaseURL("llama-%s-bin-ubuntu-x64.tar.gz"), nil
		case "arm64":
			// ggml-org publishes no ARM64 Linux build; llama-cpp-builder does.
			return llamaBuilderURL("llama-%s-bin-ubuntu-cpu-arm64.tar.gz"), nil
		}
	case "windows":
		switch goarch {
		case "amd64":
			return llamaReleaseURL("llama-%s-bin-win-cpu-x64.zip"), nil
		case "arm64":
			return llamaReleaseURL("llama-%s-bin-win-cpu-arm64.zip"), nil
		}
	}
	return "", fmt.Errorf("%w: %s/%s", errUnsupportedLocalPlatform, goos, goarch)
}

func llamaReleaseURL(nameFormat string) string {
	return fmt.Sprintf("%s/%s/%s", llamaReleaseBase, llamaNightlyTag, fmt.Sprintf(nameFormat, llamaNightlyTag))
}

func llamaBuilderURL(nameFormat string) string {
	return fmt.Sprintf("%s/%s/%s", llamaBuilderBase, llamaBuilderTag, fmt.Sprintf(nameFormat, llamaBuilderTag))
}

// localLibraryFileName is the file yzma's loader opens first. Its presence is
// what "the libraries are installed" means here.
func localLibraryFileName(goos string) string {
	switch goos {
	case "windows":
		return "llama.dll"
	case "darwin":
		return "libllama.dylib"
	default:
		return "libllama.so"
	}
}

// localLibrariesInstalled reports whether dir already holds an installation.
func localLibrariesInstalled(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	// Lstat, not Stat: the main entry is a symlink into the versioned file, and
	// a broken link is still an installation that must be replaced rather than
	// silently reported as missing.
	_, err := os.Lstat(filepath.Join(dir, localLibraryFileName(runtime.GOOS)))
	return err == nil
}

// --- integrity ---

// llamaManifestURL is the digest manifest for the pinned release. It is a var
// only because it is composed rather than written out.
var llamaManifestURL = fmt.Sprintf("%s/%s/%s.json", llamaBuilderBase, llamaBuilderTag, llamaBuilderTag)

// llamaManifest is the part of the published manifest this file reads: the
// digest of every asset, under the repository and tag that serve it.
type llamaManifest struct {
	Sources map[string]struct {
		Tag    string `json:"tag"`
		Assets map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"assets"`
	} `json:"sources"`
}

// llamaAssetURLPattern picks the repository, the release tag and the asset name
// out of a GitHub release URL. All three have to match: the same file name means
// a different file in another repository.
var llamaAssetURLPattern = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/releases/download/([^/]+)/([^?]+)$`)

// llamaAssetDigest reports the expected SHA-256 of one release archive.
//
// Every failure here stops the install. The check exists because what comes down
// is loaded into this process as native code, and "the manifest was unreachable"
// is not a reason to load something unverified — it is a reason to say so.
func llamaAssetDigest(ctx context.Context, assetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llamaManifestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "encli/"+version)

	resp, err := localDownloadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("read the llama.cpp digest manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read the llama.cpp digest manifest: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, llamaManifestLimit))
	if err != nil {
		return "", fmt.Errorf("read the llama.cpp digest manifest: %w", err)
	}
	return llamaDigestFromManifest(body, llamaManifestSHA256, assetURL)
}

// llamaDigestFromManifest authenticates the manifest bytes against the pin and
// reads one asset's digest out of them.
//
// Every step fails closed. A manifest that does not match the pin, does not
// parse, or does not name the asset all end the same way: no digest, so no
// install. Returning "" and letting the download proceed unchecked would make
// the pin decorative.
func llamaDigestFromManifest(body []byte, wantManifestSHA256, assetURL string) (string, error) {
	if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != wantManifestSHA256 {
		return "", fmt.Errorf(
			"%w: the llama.cpp digest manifest for %s has SHA-256 %s, expected %s; "+
				"refusing to install libraries that cannot be checked",
			errDigestMismatch, llamaBuilderTag, got, wantManifestSHA256)
	}

	var manifest llamaManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", fmt.Errorf("parse the llama.cpp digest manifest: %w", err)
	}

	parts := llamaAssetURLPattern.FindStringSubmatch(assetURL)
	if parts == nil {
		return "", fmt.Errorf("%q is not a GitHub release asset URL", assetURL)
	}
	source, ok := manifest.Sources[parts[1]]
	if !ok || source.Tag != parts[2] {
		return "", fmt.Errorf("the digest manifest does not cover %s at %s", parts[1], parts[2])
	}
	asset, ok := source.Assets[parts[3]]
	if !ok || asset.SHA256 == "" {
		return "", fmt.Errorf("the digest manifest gives no digest for %s", parts[3])
	}
	return asset.SHA256, nil
}

// --- unpacking ---

// isSharedLibraryName reports whether an archive entry is a shared library.
//
// The release archives also carry the llama.cpp tool suite: llama-cli,
// llama-server, ggml-rpc-server and two dozen more. They are left behind, and
// not for the disk — they link against these same libraries, so the whole
// macOS ARM64 archive unpacks to 27 MB against the 26 MB of libraries alone.
// It is that encli was asked to run a model, and unpacking a network server
// into a cache directory as a side effect of that is not the same thing.
func isSharedLibraryName(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".dll") || strings.HasSuffix(lower, ".dylib") || strings.HasSuffix(lower, ".so") {
		return true
	}
	// libggml.so.0 and libggml.so.0.24.0 are the same library under its version.
	// The suffix has to be checked rather than merely found: "notes.so.md" is
	// not a library, and the archive is remote input.
	if marker := strings.Index(lower, ".so."); marker >= 0 {
		return isLibraryVersionSuffix(lower[marker+len(".so."):])
	}
	return false
}

// libraryEntryName reduces one archive entry to the bare file name it may be
// written under, and reports whether it may be written at all.
//
// path.Base is not enough on its own. Archive paths use forward slashes, so it
// leaves a backslash inside the name alone — and filepath.Join on Windows treats
// that as a separator. An entry called `..\..\evil.dll` would survive path.Base
// whole and then land two directories above the install. The archive is remote
// input, so the name is reduced to something with no separator in it at all.
func libraryEntryName(entry string) (string, bool) {
	name := path.Base(entry)
	switch name {
	case "", ".", "..", "/":
		return "", false
	}
	if strings.ContainsAny(name, `/\`) {
		return "", false
	}
	return name, true
}

// isLibraryVersionSuffix reports whether s is a soname version tail such as
// "0" or "0.24.0".
func isLibraryVersionSuffix(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// extractSharedLibraries unpacks every shared library in archivePath into
// destDir, flattening the archive's leading directory, and reports how many
// entries it wrote.
//
// Symlinks are preserved rather than followed. The macOS and Linux archives ship
// libllama.dylib as a link to libllama.0.4.1.dylib, and the versioned file is
// what the other libraries record as their dependency: materialising the link as
// a copy would load two independent copies of the same library into the process.
func extractSharedLibraries(archivePath, destDir string) (int, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, fmt.Errorf("create %s: %w", destDir, err)
	}
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractZipLibraries(archivePath, destDir)
	}
	return extractTarGzLibraries(archivePath, destDir)
}

func extractTarGzLibraries(archivePath, destDir string) (int, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return 0, fmt.Errorf("read %s as gzip: %w", filepath.Base(archivePath), err)
	}
	defer gz.Close()

	written := 0
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return written, fmt.Errorf("read %s: %w", filepath.Base(archivePath), err)
		}
		name, ok := libraryEntryName(header.Name)
		if !ok || !isSharedLibraryName(name) {
			continue
		}
		target := filepath.Join(destDir, name)
		switch header.Typeflag {
		case tar.TypeSymlink:
			if err := writeLibrarySymlink(target, header.Linkname); err != nil {
				return written, err
			}
		case tar.TypeReg:
			if err := writeLibraryFile(target, reader); err != nil {
				return written, err
			}
		default:
			continue
		}
		written++
	}
	return written, nil
}

func extractZipLibraries(archivePath, destDir string) (int, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return 0, fmt.Errorf("read %s as zip: %w", filepath.Base(archivePath), err)
	}
	defer archive.Close()

	written := 0
	for _, entry := range archive.File {
		name, ok := libraryEntryName(entry.Name)
		if entry.FileInfo().IsDir() || !ok || !isSharedLibraryName(name) {
			continue
		}
		target := filepath.Join(destDir, name)
		body, err := entry.Open()
		if err != nil {
			return written, fmt.Errorf("read %s from %s: %w", entry.Name, filepath.Base(archivePath), err)
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			link, readErr := io.ReadAll(io.LimitReader(body, 4096))
			body.Close()
			if readErr != nil {
				return written, readErr
			}
			if err := writeLibrarySymlink(target, string(link)); err != nil {
				return written, err
			}
		} else {
			err = writeLibraryFile(target, body)
			body.Close()
			if err != nil {
				return written, err
			}
		}
		written++
	}
	return written, nil
}

// writeLibraryFile replaces target with the bytes of r. The existing file is
// removed rather than truncated: on macOS a library that is already mapped into
// some process cannot be rewritten in place, and the code-signing hash of a
// partially overwritten dylib no longer matches.
func writeLibraryFile(target string, r io.Reader) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return fmt.Errorf("write %s: %w", filepath.Base(target), err)
	}
	return out.Close()
}

// writeLibrarySymlink recreates one link, rejecting a link that would escape the
// install directory: an archive is remote input, and a "libllama.so ->
// ../../../.ssh/id_rsa" entry must not be written.
func writeLibrarySymlink(target, linkname string) error {
	if linkname == "" || filepath.IsAbs(linkname) || strings.Contains(linkname, "/") || strings.Contains(linkname, `\`) {
		return fmt.Errorf("refusing symlink %s -> %q: links must stay inside the library directory",
			filepath.Base(target), linkname)
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return os.Symlink(linkname, target)
}

// --- downloading ---

// downloadFile fetches url into dest, reporting progress as it goes and
// checking the bytes against wantSHA256 when there is one to check them against.
//
// The bytes land in a ".part" file that is renamed only once the body is
// complete and has been verified, so neither an interrupted download — a
// cancelled run, a dropped connection, a laptop lid — nor a file that fails the
// check can leave anything behind that a later run would treat as the cache.
func downloadFile(ctx context.Context, url, dest, wantSHA256 string, onProgress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dest), err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build the request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "encli/"+version)

	resp, err := localDownloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	// The scratch file carries this process's pid. encli can legitimately run
	// twice at once — a -web server and a one-shot --llm call share the cache —
	// and two downloads writing one ".part" would interleave into a file that
	// then gets renamed over the cache. Both may download; neither may corrupt
	// the other, and the rename that publishes the result is atomic.
	part := fmt.Sprintf("%s.part-%d", dest, os.Getpid())
	out, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	counter := &progressWriter{total: resp.ContentLength, onProgress: onProgress}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, counter, digest),
		io.LimitReader(resp.Body, maxLocalDownloadBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(part)
		return fmt.Errorf("download %s: %w", url, copyErr)
	}
	if closeErr != nil {
		os.Remove(part)
		return closeErr
	}
	if written > maxLocalDownloadBytes {
		os.Remove(part)
		return fmt.Errorf("download %s: the response exceeds the %s limit", url, humanBytes(maxLocalDownloadBytes))
	}
	if wantSHA256 != "" {
		if got := fmt.Sprintf("%x", digest.Sum(nil)); got != wantSHA256 {
			os.Remove(part)
			return fmt.Errorf("%w: %s has SHA-256 %s, expected %s", errDigestMismatch, url, got, wantSHA256)
		}
	}
	if err := os.Rename(part, dest); err != nil {
		os.Remove(part)
		return err
	}
	return nil
}

// localDownloadClient has no overall timeout on purpose: a 400 MB model on a
// slow link legitimately takes many minutes, and a deadline here would cancel it
// halfway every time. Cancellation is the caller's context, and a stalled
// connection is caught by the response-header timeout.
var localDownloadClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 60 * time.Second,
	},
	CheckRedirect: refuseDowngradeToHTTP,
}

// maxLocalRedirects matches net/http's own default; it is restated because
// supplying a CheckRedirect replaces that default entirely.
const maxLocalRedirects = 10

// refuseDowngradeToHTTP follows the redirects these downloads cannot do without
// — GitHub sends them to objects.githubusercontent.com, Hugging Face to its CDN
// — while refusing one that drops TLS.
//
// For everything with a pinned digest a downgrade costs only privacy, since
// substituted bytes fail the check. The path that has no digest is the one this
// is for: an operator's own LLM_LOCAL_MODEL URL. There, an https request that is
// redirected to http hands 400 MB of weights to anyone on the wire, and
// llama.cpp parses a GGUF in C.
func refuseDowngradeToHTTP(req *http.Request, via []*http.Request) error {
	if len(via) >= maxLocalRedirects {
		return fmt.Errorf("stopped after %d redirects", maxLocalRedirects)
	}
	if req.URL.Scheme != "https" && via[0].URL.Scheme == "https" {
		return fmt.Errorf("refusing a redirect from https to %s://%s", req.URL.Scheme, req.URL.Host)
	}
	return nil
}

// progressWriter reports download progress at most once a second, because the
// callback ends up on an operator's terminal or in a browser event stream and a
// line per 32 KiB chunk would drown both.
type progressWriter struct {
	done       int64
	total      int64
	last       time.Time
	onProgress func(done, total int64)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	if w.onProgress != nil && time.Since(w.last) >= time.Second {
		w.last = time.Now()
		w.onProgress(w.done, w.total)
	}
	return len(p), nil
}

// humanBytes renders a byte count for a progress line.
//
// The unit runs out at PiB rather than indexing past the end of the table: one
// of the numbers that reaches here is a Content-Length, which is whatever a
// server chose to send, and a progress line is no place to panic.
func humanBytes(n int64) string {
	const unit = 1024
	const units = "KMGTP"
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit && exp < len(units)-1; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), units[exp])
}

// --- scratch left by a process that died ---

// localScratchMaxAge is how old an abandoned scratch file has to be before it is
// swept. It is far past any download this makes and far short of interfering
// with one another process still has in flight, whose file keeps being written.
const localScratchMaxAge = time.Hour

// sweepStaleLocalScratch deletes the half-finished work of runs that were killed.
//
// The scratch names carry a pid so two encli processes cannot write to the same
// file. The cost of that is that nothing reuses them either: before, a killed
// run left one ".part" that the next run opened with O_TRUNC, and now it leaves
// a file nobody will ever look at again.
//
// -web makes that an everyday shape rather than a corner case: it starts the
// 400 MB download at launch, so someone who starts the server, sees the download
// line and presses Ctrl-C leaves a few hundred megabytes behind — silently, and
// once per attempt.
func sweepStaleLocalScratch() {
	patterns := []string{
		filepath.Join(localModelsDir(), "*.part-*"),
		filepath.Join(localPinnedModelsDir(), "*.part-*"),
		filepath.Join(localCacheDir(), "*.part-*"),
		localLibDir() + ".incomplete-*",
		filepath.Join(localCacheDir(), "[0-9]*-llama-*"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || time.Since(info.ModTime()) < localScratchMaxAge {
				continue
			}
			if err := os.RemoveAll(match); err != nil {
				debugf("local inference: could not remove abandoned %s: %v", match, err)
				continue
			}
			debugf("local inference: removed abandoned %s", match)
		}
	}
}

// --- the cached assets ---

// localAssets is what a local run needs on disk.
type localAssets struct {
	modelPath string
	libPath   string
}

// localAssetManager serialises preparation and fans progress out to everyone
// waiting on it.
//
// Both halves matter. Two chats asking for the agent at once must not start two
// downloads of the same 400 MB file, so preparation is under one lock. But a
// caller that arrives second would then sit silently for minutes behind that
// lock, which reads as a hang — so it subscribes to the progress of the download
// already running before it blocks, and reports that instead.
type localAssetManager struct {
	prepareMu sync.Mutex

	listenMu  sync.Mutex
	listeners map[int]func(string)
	nextID    int
}

var localAssetsManager = &localAssetManager{}

func (m *localAssetManager) subscribe(onProgress func(string)) func() {
	if onProgress == nil {
		return func() {}
	}
	m.listenMu.Lock()
	defer m.listenMu.Unlock()
	if m.listeners == nil {
		m.listeners = map[int]func(string){}
	}
	m.nextID++
	id := m.nextID
	m.listeners[id] = onProgress
	return func() {
		m.listenMu.Lock()
		defer m.listenMu.Unlock()
		delete(m.listeners, id)
	}
}

func (m *localAssetManager) notify(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	debugf("local inference: %s", line)

	m.listenMu.Lock()
	defer m.listenMu.Unlock()
	for _, listener := range m.listeners {
		listener(line)
	}
}

// prepare makes the libraries and the weights available, downloading whatever is
// missing, and returns where they ended up.
func (m *localAssetManager) prepare(ctx context.Context, cfg localLLMConfig, onProgress func(string)) (localAssets, error) {
	unsubscribe := m.subscribe(onProgress)
	defer unsubscribe()

	m.prepareMu.Lock()
	defer m.prepareMu.Unlock()

	sweepStaleLocalScratch()

	libPath, err := m.ensureLibraries(ctx, cfg.libPath)
	if err != nil {
		return localAssets{}, err
	}
	modelPath, err := m.ensureModel(ctx, cfg.modelRef)
	if err != nil {
		return localAssets{}, err
	}
	return localAssets{modelPath: modelPath, libPath: libPath}, nil
}

// ensureLibraries resolves the directory yzma loads llama.cpp from.
//
// A configured directory is taken as an operator's own installation and is never
// written to: someone who built llama.cpp with CUDA and pointed encli at it does
// not want a CPU build unpacked on top of it. Only the managed cache is filled.
func (m *localAssetManager) ensureLibraries(ctx context.Context, configured string) (string, error) {
	if dir := strings.TrimSpace(configured); dir != "" {
		if !localLibrariesInstalled(dir) {
			return "", fmt.Errorf("no %s in %s: point LLM_LOCAL_LIB at a llama.cpp installation, or unset it to let encli install one",
				localLibraryFileName(runtime.GOOS), dir)
		}
		return dir, nil
	}

	dir := localLibDir()
	if localLibrariesInstalled(dir) {
		return dir, nil
	}

	assetURL, err := llamaAssetURL(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", fmt.Errorf("%w; install llama.cpp yourself and set LLM_LOCAL_LIB to the directory holding %s",
			err, localLibraryFileName(runtime.GOOS))
	}

	// The digest is resolved before a byte of the archive is fetched: a manifest
	// that cannot be authenticated means the install stops, not that it proceeds
	// unchecked.
	wantDigest, err := llamaAssetDigest(ctx, assetURL)
	if err != nil {
		return "", err
	}

	m.notify("downloading the llama.cpp libraries (%s)", path.Base(assetURL))
	// Named per process and deleted below: the archive is scratch, and a second
	// encli installing at the same time must not have its copy renamed away or
	// removed out from under its unpacker.
	archive := filepath.Join(localCacheDir(), fmt.Sprintf("%d-%s", os.Getpid(), path.Base(assetURL)))
	if err := downloadFile(ctx, assetURL, archive, wantDigest, func(done, total int64) {
		m.notify("llama.cpp libraries: %s", downloadProgressText(done, total))
	}); err != nil {
		return "", err
	}
	defer os.Remove(archive)

	// Unpacked beside the target and moved into place, so an interrupted install
	// cannot leave a directory that holds libllama but not the ggml backends it
	// needs — which would pass localLibrariesInstalled and fail at dlopen. The
	// pid is there for the same reason as in downloadFile: two encli processes
	// may install at once and must not unpack into one another's directory.
	staging := fmt.Sprintf("%s.incomplete-%d", dir, os.Getpid())
	if err := os.RemoveAll(staging); err != nil {
		return "", err
	}
	written, err := extractSharedLibraries(archive, staging)
	if err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	if written == 0 {
		os.RemoveAll(staging)
		return "", fmt.Errorf("%s held no shared libraries", path.Base(assetURL))
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.Rename(staging, dir); err != nil {
		return "", err
	}
	m.notify("installed %d llama.cpp libraries in %s", written, dir)
	return dir, nil
}

// ensureModel resolves the weights, downloading them on first use.
//
// A reference that is not an http(s) URL is a path to a file the operator
// already has, and a missing one is an error rather than something to fetch:
// there is nowhere to fetch it from.
func (m *localAssetManager) ensureModel(ctx context.Context, ref string) (string, error) {
	model, err := resolveLocalModelRef(ref)
	if err != nil {
		return "", err
	}
	if model.url == "" {
		// A file the operator already has. A missing one is an error rather than
		// something to fetch: there is nowhere to fetch it from.
		info, statErr := os.Stat(model.path)
		switch {
		case statErr != nil:
			return "", fmt.Errorf("local model %s: %w", model.path, statErr)
		case !info.Mode().IsRegular():
			return "", fmt.Errorf("local model %s is not a file", model.path)
		}
		return model.path, nil
	}
	if model.present() {
		return model.path, nil
	}

	m.notify("downloading the model %s (this happens once)", model.name())
	if err := downloadFile(ctx, model.url, model.path, model.wantSHA256, func(done, total int64) {
		m.notify("model %s: %s", model.name(), downloadProgressText(done, total))
	}); err != nil {
		if errors.Is(err, errDigestMismatch) {
			return "", fmt.Errorf("%w\nThe published model no longer matches the digest encli was built with. "+
				"Download it yourself and point LLM_LOCAL_MODEL at the file if you trust it", err)
		}
		return "", err
	}
	m.notify("model ready: %s", model.path)
	return model.path, nil
}

// modelFileNameFromURL picks the cache file name for a model URL. The last path
// segment is used, and it must look like a file rather than a directory or a
// traversal, because it is joined onto the cache directory.
func modelFileNameFromURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("model URL %q: %w", raw, err)
	}
	name := path.Base(parsed.Path)
	if name == "" || name == "." || name == "/" || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("model URL %q does not end in a file name", raw)
	}
	return name, nil
}

// expandLocalPath resolves a leading ~ so a configured path can be written the
// way an operator would type it in a shell.
func expandLocalPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// downloadProgressText renders one progress line. A server that sends no
// Content-Length leaves the percentage unknowable, so only the byte count is
// reported rather than a made-up share.
func downloadProgressText(done, total int64) string {
	if total <= 0 {
		return humanBytes(done)
	}
	return fmt.Sprintf("%s / %s (%d%%)", humanBytes(done), humanBytes(total), done*100/total)
}

// localModelCached reports whether the configured model is already on disk, so
// the settings panel can say whether a first run still has a download ahead of
// it.
func localModelCached(ref string) (string, bool) {
	model, err := resolveLocalModelRef(ref)
	if err != nil {
		return "", false
	}
	return model.path, model.present()
}
