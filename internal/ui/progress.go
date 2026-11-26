package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Progress is a byte-based progress line:
//
//	│ ⠹ Downloading starter  ██████████░░░░░░░░░░░░░░  42%  1.0 MB / 2.4 MB · 1.2 MB/s
//
// On a terminal it redraws every 100 ms. Otherwise (pipe, CI, TERM=dumb) it
// prints one line at 25%, 50% and 75%. Feed it bytes through Reader or
// Writer and end it with OK or Fail.
type Progress struct {
	p     *Printer
	text  string
	total int64 // <= 0 means unknown size
	done  atomic.Int64
	start time.Time
	stop  chan struct{}
	wg    sync.WaitGroup
	once  sync.Once
}

const (
	progressTick  = 100 * time.Millisecond
	progressWidth = 24
)

// Progress starts a progress line for total bytes (<= 0 if unknown).
func (p *Printer) Progress(text string, total int64) *Progress {
	pr := &Progress{p: p, text: text, total: total, start: time.Now(), stop: make(chan struct{})}
	pr.wg.Add(1)
	go pr.loop()
	return pr
}

// Reader wraps r so every Read updates the progress.
func (pr *Progress) Reader(r io.Reader) io.Reader { return &countingReader{r: r, pr: pr} }

// Writer returns a writer that counts the bytes written to it, for use in an
// io.MultiWriter next to the real destination.
func (pr *Progress) Writer() io.Writer { return countingWriter{pr} }

type countingReader struct {
	r  io.Reader
	pr *Progress
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.pr.done.Add(int64(n))
	return n, err
}

type countingWriter struct{ pr *Progress }

func (c countingWriter) Write(b []byte) (int, error) {
	c.pr.done.Add(int64(len(b)))
	return len(b), nil
}

// pct is the percent done, or -1 if the size is unknown.
func (pr *Progress) pct() int {
	if pr.total <= 0 {
		return -1
	}
	return int(min(100, pr.done.Load()*100/pr.total))
}

func (pr *Progress) loop() {
	defer pr.wg.Done()
	t := time.NewTicker(progressTick)
	defer t.Stop()
	nextMilestone := 25

	for i := 0; ; i++ {
		if pr.p.tty {
			pr.p.mu.Lock()
			fmt.Fprint(pr.p.out, clearLine+" "+rail()+" "+violet.Render(spinnerFrames[i%len(spinnerFrames)])+" "+pr.line())
			pr.p.mu.Unlock()
		} else if pct := pr.pct(); pct >= nextMilestone && nextMilestone < 100 {
			// Plain mode: one line per quarter, no cursor tricks.
			pr.p.println(fmt.Sprintf(" %s … %s %d%%", rail(), pr.text, nextMilestone))
			nextMilestone += 25
		}
		select {
		case <-pr.stop:
			return
		case <-t.C:
		}
	}
}

// line renders the bar, or bytes received when the size is unknown.
func (pr *Progress) line() string {
	done := pr.done.Load()
	pct := pr.pct()
	if pct < 0 {
		return fmt.Sprintf("%s… %s", pr.text, dim.Render(HumanBytes(done)))
	}
	filled := pct * progressWidth / 100
	bar := green.Render(strings.Repeat("█", filled)) + dim.Render(strings.Repeat("░", progressWidth-filled))

	speed := float64(done) / max(time.Since(pr.start).Seconds(), 0.1)
	return fmt.Sprintf("%s  %s %s  %s", pr.text, bar, bold.Render(fmt.Sprintf("%3d%%", pct)),
		dim.Render(fmt.Sprintf("%s / %s · %s/s", HumanBytes(done), HumanBytes(pr.total), HumanBytes(int64(speed)))))
}

func (pr *Progress) finish(line string) {
	pr.once.Do(func() {
		close(pr.stop)
		pr.wg.Wait()
		pr.p.mu.Lock()
		defer pr.p.mu.Unlock()
		if pr.p.tty {
			fmt.Fprint(pr.p.out, clearLine)
		}
		fmt.Fprintln(pr.p.out, line)
	})
}

// OK replaces the progress line with "✓ text  <size> in <time>".
func (pr *Progress) OK(text string) {
	took := fmt.Sprintf("%.1fs", time.Since(pr.start).Seconds())
	pr.finish(fmt.Sprintf(" %s %s %s  %s", rail(), green.Render("✓"), text,
		dim.Render(HumanBytes(pr.done.Load())+" in "+took)))
}

// Fail replaces the progress line with "✗ text".
func (pr *Progress) Fail(text string) {
	pr.finish(fmt.Sprintf(" %s %s %s", rail(), red.Render("✗"), red.Render(text)))
}

// HumanBytes formats n as B, KB or MB.
func HumanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
