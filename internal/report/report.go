// Package report renders evaluation progress and results for the terminal.
package report

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type Status int

const (
	Pass Status = iota
	Fail
	Inconclusive
	Skipped
)

type Result struct {
	Level    string // "L0", "L1", ...
	Name     string // "build", "ocp-discount"
	Note     string // "16/16", "(4 frozen symbols)"
	Status   Status
	Duration time.Duration // set by Live from Start→Done; 0 = not printed

	// Shown under the stage only when it fails or is inconclusive.
	Reason  string   // summary: "1/8 hidden tests failed"
	Issues  []Issue  // where it went wrong, one line each
	Details []string // raw tool output, shown when there are no Issues
	Reasons []string // likely causes for the learner to reason about
	Hint    string
}

// Issue is one problem at a place in the submission.
type Issue struct {
	Where string // "order.go:42"; empty when the problem has no position
	Note  string // "undefined: calculateTotal"
}

// maxDetailLines caps raw tool output under a stage.
const maxDetailLines = 20

var levelTitles = map[string]string{
	"L0": "Hygiene", "L1": "Contract", "L2": "Behaviour", "L3": "Design",
}

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15"))
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("#71717a"))
	bold   = lipgloss.NewStyle().Bold(true)
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("#67e8f9")).Bold(true)
	violet = lipgloss.NewStyle().Foreground(lipgloss.Color("#c4b5fd")).Bold(true)

	tag     = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	runTag  = tag.Background(lipgloss.Color("#3b0764")).Foreground(lipgloss.Color("#e9d5ff"))
	passTag = tag.Background(lipgloss.Color("#14532d")).Foreground(lipgloss.Color("#bbf7d0"))
	failTag = tag.Background(lipgloss.Color("#7f1d1d")).Foreground(lipgloss.Color("#fecaca"))
)

const nameWidth = 38

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	spinnerTick = 100 * time.Millisecond
	clearLine   = "\r\033[2K"
)

// Live prints stages one by one as they run: a spinner line while a stage is
// running, replaced by its result (and error output) when it finishes.
// On a non-terminal writer there is no spinner; each row is printed once done.
type Live struct {
	w   io.Writer
	tty bool

	mu     sync.Mutex
	levels map[string]bool // level headers already printed

	// running stage (zero name = nothing running)
	runName  string
	runStart time.Time
	frame    int
	stop     chan struct{}
	stopped  chan struct{}

	passed, failed, inconc, skipped int
}

// NewLive prints the RUN header. tty enables the spinner.
func NewLive(w io.Writer, tty bool, assignment, submission string) *Live {
	fmt.Fprintf(w, " %s %s %s\n\n", runTag.Render("RUN"), bold.Render(assignment), dim.Render("› "+submission))
	return &Live{w: w, tty: tty, levels: map[string]bool{}}
}

// Start marks a stage as running.
func (l *Live) Start(level, name string) {
	l.stopSpinner()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.header(level)
	l.runName, l.runStart, l.frame = name, time.Now(), 0
	if !l.tty {
		return
	}
	l.drawSpinner()
	l.stop, l.stopped = make(chan struct{}), make(chan struct{})
	go l.spin(l.stop, l.stopped)
}

// Done replaces the running line with the stage's result. Stages that never
// started (skipped after a failed gate) are printed directly.
func (l *Live) Done(r Result) {
	l.stopSpinner()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.runName == r.Name && r.Duration == 0 && r.Status != Skipped {
		r.Duration = time.Since(l.runStart)
	}
	l.runName = ""
	if l.tty {
		fmt.Fprint(l.w, clearLine)
	}
	l.header(r.Level)
	l.writeRow(r)
}

// Finish prints the PASS/FAIL summary.
func (l *Live) Finish(total time.Duration) {
	l.stopSpinner()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.tty && l.runName != "" {
		fmt.Fprint(l.w, clearLine) // a stage started but never reported (evaluator error)
	}
	status := passTag.Render("PASS")
	if l.failed > 0 {
		status = failTag.Render("FAIL")
	}
	failStr := fmt.Sprintf("%d failed", l.failed)
	if l.failed > 0 {
		failStr = red.Render(failStr)
	} else {
		failStr = dim.Render(failStr)
	}
	fmt.Fprintf(l.w, "\n %s  %s %s %s %s\n", status,
		green.Render(fmt.Sprintf("%d passed", l.passed)), dim.Render("·"), failStr,
		dim.Render(fmt.Sprintf("· %d inconclusive · %d skipped", l.inconc, l.skipped)))
	fmt.Fprintln(l.w, dim.Render(" Time  "+fmtDur(total)))
}

func (l *Live) spin(stop <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	t := time.NewTicker(spinnerTick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			l.mu.Lock()
			l.frame++
			l.drawSpinner()
			l.mu.Unlock()
		}
	}
}

// stopSpinner ends the spinner goroutine (if any) and waits for it.
func (l *Live) stopSpinner() {
	l.mu.Lock()
	stop, stopped := l.stop, l.stopped
	l.stop, l.stopped = nil, nil
	l.mu.Unlock()
	if stop != nil {
		close(stop)
		<-stopped
	}
}

// drawSpinner redraws the running line in place. Caller holds mu.
func (l *Live) drawSpinner() {
	icon := cyan.Render(spinnerFrames[l.frame%len(spinnerFrames)])
	label := l.runName + " " + dim.Render("running…")
	fmt.Fprintf(l.w, "%s %s %s %s%s", clearLine, dim.Render("│"), icon, label,
		padded(label, fmtDur(time.Since(l.runStart))))
}

// header prints the level title the first time the level appears. Caller holds mu.
func (l *Live) header(level string) {
	if l.levels[level] {
		return
	}
	l.levels[level] = true
	h := fmt.Sprintf("%s · %s", level, levelTitles[level])
	if level == "L3" {
		fmt.Fprintln(l.w, " "+violet.Render(h))
	} else {
		fmt.Fprintln(l.w, " "+cyan.Render(h))
	}
}

// writeRow prints a finished stage and, on failure, its error output. Caller holds mu.
func (l *Live) writeRow(r Result) {
	bar := dim.Render("│")
	icon, name := green.Render("✓"), r.Name
	switch r.Status {
	case Pass:
		l.passed++
	case Fail:
		l.failed++
		icon, name = red.Render("✗"), red.Render(r.Name)
	case Inconclusive:
		l.inconc++
		icon = yellow.Render("?")
	case Skipped:
		l.skipped++
		icon, name = dim.Render("○"), dim.Render(r.Name)
	}

	label := name
	if r.Note != "" {
		label += " " + dim.Render(r.Note)
	}
	dur := ""
	if r.Duration > 0 {
		dur = padded(label, fmtDur(r.Duration))
	}
	fmt.Fprintf(l.w, " %s %s %s%s\n", bar, icon, label, dur)

	if r.Status == Fail || r.Status == Inconclusive {
		renderError(l.w, bar, r)
	}
}

// padded right-aligns a dimmed duration after label.
func padded(label, dur string) string {
	pad := nameWidth - lipgloss.Width(label)
	if pad < 1 {
		pad = 1
	}
	return strings.Repeat(" ", pad) + dim.Render(dur)
}

// renderError prints a stage's own error output directly under its row.
func renderError(w io.Writer, bar string, r Result) {
	arrow := red.Render("→")
	if r.Status == Inconclusive {
		arrow = yellow.Render("→")
	}
	if r.Reason != "" {
		fmt.Fprintf(w, " %s   %s %s\n", bar, arrow, r.Reason)
	}
	for _, is := range r.Issues {
		if is.Where == "" {
			fmt.Fprintf(w, " %s     %s\n", bar, is.Note)
			continue
		}
		fmt.Fprintf(w, " %s     %s %s\n", bar, yellow.Render(is.Where), is.Note)
	}
	if len(r.Issues) == 0 && len(r.Details) > 0 {
		lines := r.Details
		if len(lines) > maxDetailLines {
			lines = lines[:maxDetailLines]
		}
		for _, l := range lines {
			fmt.Fprintf(w, " %s     %s %s\n", bar, dim.Render("┆"), l)
		}
		if more := len(r.Details) - len(lines); more > 0 {
			fmt.Fprintf(w, " %s     %s\n", bar, dim.Render(fmt.Sprintf("┆ … %d more lines (--format json for all)", more)))
		}
	}
	for _, reason := range r.Reasons {
		fmt.Fprintf(w, " %s     %s\n", bar, dim.Render("• "+reason))
	}
	if r.Hint != "" {
		fmt.Fprintf(w, " %s     %s\n", bar, dim.Render("hint: "+r.Hint))
	}
}

func fmtDur(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}
