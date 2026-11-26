package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestConfirm(t *testing.T) {
	for input, want := range map[string]bool{
		"\n": true, "y\n": true, "Y\n": true, "yes\n": true, " YES \n": true, "y": true, // EOF after an answer
		"n\n": false, "no\n": false, "nope\n": false, "": false, // EOF without an answer
	} {
		var out bytes.Buffer
		p := New(&out, strings.NewReader(input), WithInteractive(true))
		got, err := p.Confirm(context.Background(), "Sign in again now?")
		if err != nil || got != want {
			t.Errorf("input %q: got %v, %v; want %v", input, got, err, want)
		}
		if !strings.Contains(out.String(), "│   Sign in again now? [Y/n] ") {
			t.Errorf("input %q: prompt missing: %q", input, out.String())
		}
	}
}

func TestConfirm_NonInteractiveNeverReads(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	p := New(&out, in) // a buffer is not a terminal
	if p.Interactive() {
		t.Fatal("buffer should not be interactive")
	}
	if got, err := p.Confirm(context.Background(), "Sign in?"); got || err != nil {
		t.Fatalf("got %v, %v", got, err)
	}
	if out.Len() != 0 || in.Len() != 2 {
		t.Fatalf("should neither prompt nor read: out=%q unread=%d", out.String(), in.Len())
	}
}

func TestConfirm_CancelledWhileWaiting(t *testing.T) {
	r, w := io.Pipe() // never written: the user hasn't answered
	defer w.Close()
	p := New(io.Discard, r, WithInteractive(true))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	if _, err := p.Confirm(ctx, "Sign in?"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRailLines(t *testing.T) {
	var out bytes.Buffer
	p := New(&out, strings.NewReader(""))
	p.Tag("START", "GO-SRP-01")
	p.Warn("Your session expired")
	p.OK("Opened your browser")
	p.Fail("Session expired")
	p.Reason("your saved login is no longer valid", "run devcraft login")
	p.Failed("Couldn't start assignment")
	p.Done("cd GO-SRP-01", "/tmp/GO-SRP-01")

	want := []string{
		" START  GO-SRP-01",
		" │ ! Your session expired",
		" │ ✓ Opened your browser",
		" │ ✗ Session expired",
		" │   → your saved login is no longer valid",
		" │     hint: run devcraft login",
		" FAIL   Couldn't start assignment",
		" DONE   cd GO-SRP-01",
		"         /tmp/GO-SRP-01",
	}
	for _, w := range want {
		if !strings.Contains(out.String(), w) {
			t.Errorf("missing %q in\n%s", w, out.String())
		}
	}
}

func TestStep_NonTerminalPrintsOnceWithDuration(t *testing.T) {
	var out bytes.Buffer
	p := New(&out, strings.NewReader(""))
	s := p.Start("Downloading")
	if out.Len() != 0 {
		t.Fatalf("no spinner without a terminal: %q", out.String())
	}
	s.OK("Downloaded")
	s.Fail("ignored: a step ends once")
	if got := out.String(); !strings.HasPrefix(got, " │ ✓ Downloaded ") || !strings.HasSuffix(got, "0:00\n") || strings.Count(got, "\n") != 1 {
		t.Fatalf("got %q", got)
	}
}

func TestClock(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0:00", 9 * time.Second: "0:09", 61 * time.Second: "1:01", 1500 * time.Millisecond: "0:02",
	} {
		if got := clock(d); got != want {
			t.Errorf("clock(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTable(t *testing.T) {
	var out bytes.Buffer
	New(&out, strings.NewReader("")).Table(
		[]string{"CODE", "NAME"},
		[][]string{{"GO-SRP-01", "Order processor"}, {"GO-A", "Short"}},
	)
	want := " │ CODE        NAME\n" +
		" │ GO-SRP-01   Order processor\n" +
		" │ GO-A        Short\n"
	if out.String() != want {
		t.Fatalf("got\n%q\nwant\n%q", out.String(), want)
	}
}

func TestAsk(t *testing.T) {
	for input, want := range map[string]string{
		"\n":              "~/devcraft", // Enter takes the default
		"  ~/code/dc  \n": "~/code/dc",
		"/abs/path":       "/abs/path", // EOF after an answer
	} {
		var out bytes.Buffer
		p := New(&out, strings.NewReader(input), WithInteractive(true))
		got, err := p.Ask(context.Background(), "Where should DevCraft keep your assignments?", "Folder", "~/devcraft")
		if err != nil || got != want {
			t.Errorf("input %q: got %q, %v; want %q", input, got, err, want)
		}
		for _, w := range []string{" │ ? Where should DevCraft keep your assignments?\n", " │   Folder [~/devcraft]: "} {
			if !strings.Contains(out.String(), w) {
				t.Errorf("input %q: missing %q in %q", input, w, out.String())
			}
		}
	}
}

func TestAsk_NoInput(t *testing.T) {
	p := New(io.Discard, strings.NewReader(""), WithInteractive(true))
	if _, err := p.Ask(context.Background(), "", "Folder", "x"); !errors.Is(err, ErrNoInput) {
		t.Fatalf("EOF: err = %v, want ErrNoInput", err)
	}
	p = New(io.Discard, strings.NewReader("y\n")) // not a terminal
	if _, err := p.Ask(context.Background(), "", "Folder", "x"); !errors.Is(err, ErrNoInput) {
		t.Fatalf("no terminal: err = %v, want ErrNoInput", err)
	}
}

func TestAsk_Cancelled(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	p := New(io.Discard, r, WithInteractive(true))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := p.Ask(ctx, "", "Folder", "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
