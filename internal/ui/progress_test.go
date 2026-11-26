package ui

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe to read while the progress goroutine writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls out until it contains want.
func waitFor(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in\n%q", want, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProgress_PlainPrintsQuarterMilestones(t *testing.T) {
	out := &syncBuffer{}
	p := New(out, strings.NewReader("")) // not a terminal
	pr := p.Progress("Downloading starter", 100)
	w := pr.Writer()

	_, _ = w.Write(make([]byte, 30))
	waitFor(t, out, " │ … Downloading starter 25%\n")
	_, _ = w.Write(make([]byte, 50)) // 80%
	waitFor(t, out, " │ … Downloading starter 50%\n")
	waitFor(t, out, " │ … Downloading starter 75%\n")
	pr.OK("Downloaded GO-SRP-01 starter")

	got := out.String()
	if strings.Contains(got, "\r") || strings.Contains(got, "█") {
		t.Fatalf("plain mode must not redraw or draw a bar:\n%q", got)
	}
	if strings.Count(got, "25%") != 1 || strings.Contains(got, "100%") {
		t.Fatalf("each milestone once, never 100%%:\n%s", got)
	}
	if !strings.HasSuffix(got, " │ ✓ Downloaded GO-SRP-01 starter  80 B in 0.0s\n") &&
		!strings.Contains(got, " │ ✓ Downloaded GO-SRP-01 starter  80 B in ") {
		t.Fatalf("final line:\n%q", got)
	}
}

func TestProgress_FastDownloadPrintsOnlyResult(t *testing.T) {
	out := &syncBuffer{}
	p := New(out, strings.NewReader(""))
	pr := p.Progress("Downloading starter", 2048)
	_, _ = io.Copy(io.Discard, pr.Reader(bytes.NewReader(make([]byte, 2048))))
	pr.OK("Downloaded")
	pr.Fail("ignored: ends once")

	if got := out.String(); strings.Count(got, "\n") > 4 || !strings.Contains(got, "✓ Downloaded  2 KB in ") || strings.Contains(got, "✗") {
		t.Fatalf("got %q", got)
	}
}

func TestProgress_TerminalRedrawsBar(t *testing.T) {
	out := &syncBuffer{}
	p := New(out, strings.NewReader(""), WithTerminal(true))
	pr := p.Progress("Downloading starter", 100)
	_, _ = pr.Writer().Write(make([]byte, 42))
	waitFor(t, out, " 42%")
	pr.Fail("Download failed")

	got := out.String()
	for _, want := range []string{clearLine, "Downloading starter  ", "██████████░", "42 B / 100 B", "B/s"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if !strings.HasSuffix(got, clearLine+" │ ✗ Download failed\n") {
		t.Fatalf("the bar must be cleared and replaced by the result:\n%q", got)
	}
}

func TestProgress_UnknownSizeShowsBytes(t *testing.T) {
	out := &syncBuffer{}
	p := New(out, strings.NewReader(""), WithTerminal(true))
	pr := p.Progress("Downloading starter", -1)
	_, _ = pr.Writer().Write(make([]byte, 1536))
	waitFor(t, out, "Downloading starter… 2 KB")
	pr.OK("Downloaded")
	if strings.Contains(out.String(), "%") || strings.Contains(out.String(), "█") {
		t.Fatalf("no bar or percent without a size:\n%q", out.String())
	}
}

func TestProgress_PercentCapsAt100(t *testing.T) {
	pr := &Progress{total: 10}
	pr.done.Store(25)
	if got := pr.pct(); got != 100 {
		t.Fatalf("pct = %d", got)
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 1023: "1023 B", 1024: "1 KB", 9497: "9 KB", 1 << 20: "1.0 MB", 2516582: "2.4 MB",
	} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
