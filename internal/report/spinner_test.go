package report

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe for the spinner goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestSpinnerPlainPrintsStatusChangesOnce(t *testing.T) {
	var b bytes.Buffer
	s := NewSpinner(&b, false, "Evaluation e1")
	for _, st := range []string{"queued", "queued", "running", "running", "completed"} {
		s.Set(st)
	}
	s.Stop()
	s.Stop() // idempotent

	want := "Evaluation e1: queued\nEvaluation e1: running\nEvaluation e1: completed\n"
	if b.String() != want {
		t.Fatalf("output = %q, want %q", b.String(), want)
	}
}

func TestSpinnerTTYAnimatesAndClears(t *testing.T) {
	var b syncBuffer
	s := NewSpinner(&b, true, "Evaluation e1")
	s.Set("running")
	time.Sleep(3 * spinnerTick)
	s.Stop()
	s.Stop()

	out := b.String()
	if n := strings.Count(out, clearLine); n < 3 {
		t.Fatalf("expected the line to be redrawn several times, got %d redraws", n)
	}
	if !strings.HasSuffix(out, clearLine) {
		t.Fatal("Stop must leave the line cleared")
	}
	if strings.Contains(out, "\n") {
		t.Fatal("tty spinner must stay on one line")
	}
}
