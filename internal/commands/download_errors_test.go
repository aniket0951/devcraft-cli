package commands

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aniket0951/devcraft-cli.git/internal/ui"
	"github.com/spf13/cobra"
)

// startFailsWith runs start and checks the learner-facing reason, that no
// technical detail leaks, and that nothing is left behind.
func startFailsWith(t *testing.T, f *fakeAPI, why, hint string) {
	t.Helper()
	cwd := chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err == nil {
		t.Fatalf("expected failure\n%s", out)
	}
	assertContains(t, out, "✗ Download failed", "→ "+why, "hint: "+hint, "Couldn't start assignment")
	assertNotContains(t, out, "127.0.0.1", "dial", "tcp", "Get", "X-Amz", "storage", "api error", "download:")
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestStart_DownloadServerUnreachable(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	// a port nothing listens on: the connection is refused, like a blocked host
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.downloadURL = "http://" + l.Addr().String() + "/starter.zip?X-Amz-Signature=secret"
	_ = l.Close()

	startFailsWith(t, f, "couldn't connect to the download server",
		"check your internet connection, VPN or firewall, then run this command again")
}

func TestStart_DownloadServerNotFound(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.downloadURL = "http://devcraft-starter.invalid/starter.zip" // .invalid never resolves

	startFailsWith(t, f, "couldn't connect to the download server", "check your internet connection")
}

func TestStart_DownloadLinkExpired(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.storage = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<Error><Code>AccessDenied</Code><Message>Request has expired</Message></Error>", http.StatusForbidden)
	}
	startFailsWith(t, f, "the download link expired", "run this command again")
}

func TestStart_DownloadServerError(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.storage = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<Error><Code>InternalError</Code></Error>", http.StatusInternalServerError)
	}
	startFailsWith(t, f, "the download server returned an error", "try again in a few minutes")
}

func TestStart_DownloadInterrupted(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.storage = func(w http.ResponseWriter, r *http.Request) {
		// promise the whole file, send half, then drop the connection
		w.Header().Set("Content-Length", strconv.Itoa(len(f.starter)))
		_, _ = w.Write(f.starter[:len(f.starter)/2])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}
	startFailsWith(t, f, "the download was interrupted", "check your internet connection, then run this command again")
}

func assertNotContains(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			t.Errorf("output must not contain %q\n--- output ---\n%s", sub, s)
		}
	}
}

func TestStart_ProgressBarOnTerminal(t *testing.T) {
	loginForTest(t)
	f := newFakeAPI(t)
	f.storage = func(w http.ResponseWriter, r *http.Request) {
		// send in two halves with a pause so the bar redraws mid-download
		half := len(f.starter) / 2
		w.Header().Set("Content-Length", strconv.Itoa(len(f.starter)))
		_, _ = w.Write(f.starter[:half])
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write(f.starter[half:])
	}
	orig := newPrinter
	t.Cleanup(func() { newPrinter = orig })
	newPrinter = func(cmd *cobra.Command) *ui.Printer {
		return ui.New(cmd.OutOrStdout(), strings.NewReader(""), ui.WithTerminal(true))
	}
	chdirAssignments(t)

	out, err := runCLI(t, f, "start", "GO-SOLID-001", "--no-git")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	assertContains(t, out, "Downloading starter  ", "█", "░", " 50%", "162 B / 324 B", "✓ Downloaded GO-SOLID-001 v1.2.0 starter  324 B in ")
}
