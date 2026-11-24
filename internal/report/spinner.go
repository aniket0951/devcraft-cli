package report

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Spinner shows a single "waiting" line, e.g. while polling a remote
// evaluation. On a terminal it animates in place; otherwise it prints
// "<label>: <status>" once each time the status changes.
type Spinner struct {
	w     io.Writer
	tty   bool
	label string

	mu      sync.Mutex
	status  string
	start   time.Time
	frame   int
	stop    chan struct{}
	stopped chan struct{}
}

// NewSpinner starts a spinner with the given label.
func NewSpinner(w io.Writer, tty bool, label string) *Spinner {
	s := &Spinner{w: w, tty: tty, label: label, start: time.Now()}
	if tty {
		s.stop, s.stopped = make(chan struct{}), make(chan struct{})
		s.draw()
		go s.spin(s.stop, s.stopped)
	}
	return s
}

// Set updates the status text shown next to the label.
func (s *Spinner) Set(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == s.status {
		return
	}
	s.status = status
	if s.tty {
		s.draw()
		return
	}
	fmt.Fprintf(s.w, "%s: %s\n", s.label, status)
}

// Stop ends the animation and clears the line. Safe to call more than once.
func (s *Spinner) Stop() {
	s.mu.Lock()
	stop, stopped := s.stop, s.stopped
	s.stop, s.stopped = nil, nil
	s.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-stopped
	fmt.Fprint(s.w, clearLine)
}

func (s *Spinner) spin(stop <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	t := time.NewTicker(spinnerTick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.mu.Lock()
			s.frame++
			s.draw()
			s.mu.Unlock()
		}
	}
}

// draw redraws the line in place. Caller holds mu (or owns s exclusively).
func (s *Spinner) draw() {
	icon := cyan.Render(spinnerFrames[s.frame%len(spinnerFrames)])
	label := s.label
	if s.status != "" {
		label += " " + dim.Render(s.status+"…")
	}
	fmt.Fprintf(s.w, "%s %s %s%s", clearLine, icon, label, padded(label, fmtDur(time.Since(s.start))))
}
