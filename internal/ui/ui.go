// Package ui prints rail-style command output:
//
//	START  GO-SRP-01
//
//	│ ! Your session expired
//	│   Sign in again now? [Y/n] y
//
//	│ ✓ Signed in as a@b.dev                 0:09
//	│ ✓ Downloaded GO-SRP-01 v1.0.0          0:01
//
//	DONE  cd GO-SRP-01
//
// It uses the same palette as package report.
package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("#67e8f9")).Bold(true)
	violet = lipgloss.NewStyle().Foreground(lipgloss.Color("#c4b5fd")).Bold(true)
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("#71717a"))
	bold   = lipgloss.NewStyle().Bold(true)

	tag     = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	authTag = tag.Background(lipgloss.Color("#1e3a8a")).Foreground(lipgloss.Color("#bfdbfe"))
	doneTag = tag.Background(lipgloss.Color("#14532d")).Foreground(lipgloss.Color("#bbf7d0"))
	failTag = tag.Background(lipgloss.Color("#7f1d1d")).Foreground(lipgloss.Color("#fecaca"))
)

const (
	labelWidth  = 38 // durations are right-aligned after this many columns
	spinnerTick = 100 * time.Millisecond
	clearLine   = "\r\033[2K"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Bold and Dim style inline text.
func Bold(s string) string { return bold.Render(s) }
func Dim(s string) string  { return dim.Render(s) }

func rail() string { return dim.Render("│") }

// Printer writes rail-style output to out and reads answers from in.
type Printer struct {
	out         io.Writer
	in          *bufio.Reader
	tty         bool // out is a terminal (and not CI or TERM=dumb): animate steps and progress
	interactive bool // safe to ask questions

	mu sync.Mutex
}

// Option configures a Printer.
type Option func(*Printer)

// WithInteractive overrides terminal detection for prompts (tests).
func WithInteractive(v bool) Option { return func(p *Printer) { p.interactive = v } }

// WithTerminal overrides terminal detection for animation (tests).
func WithTerminal(v bool) Option { return func(p *Printer) { p.tty = v } }

// New returns a Printer. Animation needs out to be a terminal; prompts need
// out and in to be terminals. Neither happens when CI is set or TERM is
// "dumb": then Confirm never blocks and returns false.
func New(out io.Writer, in io.Reader, opts ...Option) *Printer {
	plain := os.Getenv("TERM") == "dumb" || os.Getenv("CI") != ""
	p := &Printer{
		out:         out,
		in:          bufio.NewReader(in),
		tty:         IsTerminal(out) && !plain,
		interactive: IsTerminal(out) && IsTerminal(in) && !plain,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// IsTerminal reports whether v is a character device such as a terminal.
func IsTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Interactive reports whether Confirm will ask.
func (p *Printer) Interactive() bool { return p.interactive }

func (p *Printer) println(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintln(p.out, s)
}

// Tag prints a header with any label: START, AUTH, RUN...
func (p *Printer) Tag(label, title string) {
	p.println(" " + authTag.Render(label) + " " + bold.Render(title) + "\n")
}

// Blank prints an empty line.
func (p *Printer) Blank() { p.println("") }

// Warn prints " │ ! text".
func (p *Printer) Warn(text string) {
	p.println(fmt.Sprintf(" %s %s %s", rail(), yellow.Render("!"), text))
}

// OK prints " │ ✓ text".
func (p *Printer) OK(text string) {
	p.println(fmt.Sprintf(" %s %s %s", rail(), green.Render("✓"), text))
}

// Fail prints " │ ✗ title".
func (p *Printer) Fail(title string) {
	p.println(fmt.Sprintf(" %s %s %s", rail(), red.Render("✗"), red.Render(title)))
}

// Line prints an indented, unmarked line on the rail (e.g. a URL to open).
func (p *Printer) Line(text string) {
	p.println(fmt.Sprintf(" %s   %s", rail(), text))
}

// Reason prints why something failed and, when hint is set, what to do.
func (p *Printer) Reason(why, hint string) {
	if why != "" {
		p.println(fmt.Sprintf(" %s   %s %s", rail(), red.Render("→"), why))
	}
	if hint != "" {
		p.println(fmt.Sprintf(" %s     %s", rail(), dim.Render("hint: ")+hint))
	}
}

// Failed prints the closing FAIL line.
func (p *Printer) Failed(msg string) {
	p.println("\n " + failTag.Render("FAIL") + "  " + msg)
}

// Done prints the closing DONE line; extra lines are aligned under text.
func (p *Printer) Done(text string, extra ...string) {
	p.println("\n " + doneTag.Render("DONE") + "  " + text)
	for _, e := range extra {
		p.println("         " + e) // under text: " " + " DONE " + "  "
	}
}

// ErrNoInput is returned by Ask when there is no terminal to ask in, or the
// input ended without an answer.
var ErrNoInput = errors.New("no answer")

// Confirm asks a yes/no question; Enter means yes. Without a terminal it
// returns false so nothing ever blocks waiting for input. It returns
// ctx.Err() if ctx is cancelled (Ctrl+C) while waiting.
func (p *Printer) Confirm(ctx context.Context, question string) (bool, error) {
	if !p.interactive {
		return false, nil
	}
	p.mu.Lock()
	fmt.Fprintf(p.out, " %s   %s %s ", rail(), question, bold.Render("[Y/n]"))
	p.mu.Unlock()

	line, err := p.readLine(ctx)
	p.Blank()
	if errors.Is(err, ErrNoInput) {
		return false, nil // EOF: no answer is not a yes
	}
	if err != nil {
		return false, err
	}
	answer := strings.ToLower(line)
	return answer == "" || answer == "y" || answer == "yes", nil
}

// Ask asks for one line of text:
//
//	│ ? Where should DevCraft keep your assignments?
//	│   Folder [~/devcraft]:
//
// Enter takes def. It returns ErrNoInput without a terminal or when the
// input ends without an answer, and ctx.Err() on Ctrl+C.
func (p *Printer) Ask(ctx context.Context, question, label, def string) (string, error) {
	if !p.interactive {
		return "", ErrNoInput
	}
	p.mu.Lock()
	if question != "" {
		fmt.Fprintf(p.out, " %s %s %s\n", rail(), cyan.Render("?"), question)
	}
	prompt := label
	if def != "" {
		prompt += " " + dim.Render("["+def+"]")
	}
	fmt.Fprintf(p.out, " %s   %s: ", rail(), prompt)
	p.mu.Unlock()

	line, err := p.readLine(ctx)
	if err != nil {
		p.Blank()
		return "", err
	}
	if line == "" {
		return def, nil
	}
	return line, nil
}

// readLine reads one trimmed line, giving up when ctx is cancelled. A line
// cut short by EOF still counts; EOF with nothing typed is ErrNoInput.
func (p *Printer) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := p.in.ReadString('\n')
		ch <- result{line, err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil && r.line == "" {
			return "", ErrNoInput
		}
		return strings.TrimSpace(r.line), nil
	}
}

// Step is a running action: a spinner on a terminal, then one ✓/✗ line
// with the elapsed time.
type Step struct {
	p       *Printer
	label   string
	start   time.Time
	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

// Start begins a step. Nothing else may be printed until it ends.
func (p *Printer) Start(label string) *Step {
	s := &Step{p: p, label: label, start: time.Now()}
	if p.tty {
		s.stop, s.stopped = make(chan struct{}), make(chan struct{})
		go s.spin()
	}
	return s
}

// OK ends the step successfully.
func (s *Step) OK(text string) { s.end(green.Render("✓"), text) }

// Fail ends the step as failed.
func (s *Step) Fail(text string) { s.end(red.Render("✗"), red.Render(text)) }

func (s *Step) end(icon, text string) {
	s.once.Do(func() {
		if s.stop != nil {
			close(s.stop)
			<-s.stopped
		}
		s.p.mu.Lock()
		defer s.p.mu.Unlock()
		if s.p.tty {
			fmt.Fprint(s.p.out, clearLine)
		}
		fmt.Fprintf(s.p.out, " %s %s %s%s\n", rail(), icon, text, padded(text, time.Since(s.start)))
	})
}

func (s *Step) spin() {
	defer close(s.stopped)
	t := time.NewTicker(spinnerTick)
	defer t.Stop()
	for frame := 0; ; frame++ {
		s.p.mu.Lock()
		icon := cyan.Render(spinnerFrames[frame%len(spinnerFrames)])
		fmt.Fprintf(s.p.out, "%s %s %s %s%s", clearLine, rail(), icon, s.label, padded(s.label, time.Since(s.start)))
		s.p.mu.Unlock()
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
	}
}

// padded right-aligns a dimmed m:ss duration after label.
func padded(label string, d time.Duration) string {
	pad := labelWidth - lipgloss.Width(label)
	if pad < 1 {
		pad = 1
	}
	return strings.Repeat(" ", pad) + dim.Render(clock(d))
}

// clock formats d as m:ss.
func clock(d time.Duration) string {
	sec := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// Table prints rows as aligned columns on the rail, with a dimmed header.
func (p *Printer) Table(headers []string, rows [][]string) {
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()

	// Style after aligning: ANSI codes would throw tabwriter's widths off.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " ")
		if i == 0 {
			line = dim.Render(line)
		}
		p.println(fmt.Sprintf(" %s %s", rail(), line))
	}
}
