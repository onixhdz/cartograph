package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// spinner provides an animated terminal spinner for long-running operations.
// It writes to stdout and uses \r-based overwriting on TTYs.
type spinner struct {
	mu      sync.Mutex
	msg     string
	start   time.Time
	running bool
	done    chan struct{}
	stopped chan struct{} // closed when animation goroutine exits
	isTTY   bool
}

// Braille spinner frames – smooth and compact.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// newSpinner creates a spinner with the given message. The spinner
// does NOT start automatically — call Start().
func newSpinner(msg string) *spinner {
	return &spinner{
		msg:     msg,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
		isTTY:   term.IsTerminal(int(os.Stdout.Fd())), //nolint:gosec // G115: fd is a small integer
	}
}

// write outputs s to stdout immediately, bypassing any buffering.
func write(s string) {
	_, _ = os.Stdout.WriteString(s)
}

// Start begins the spinner animation in a background goroutine.
func (s *spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.running = true
	s.start = time.Now()

	if !s.isTTY {
		// Non-interactive: just print the message once.
		write(fmt.Sprintf("  %s...\n", s.msg))
		close(s.stopped)
		return
	}

	// Render first frame immediately — don't wait for ticker.
	write(fmt.Sprintf("\r  %s %s", spinnerFrames[0], s.msg))

	go func() {
		defer close(s.stopped)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		i := 1
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.mu.Lock()
				if !s.running {
					s.mu.Unlock()
					return
				}
				elapsed := time.Since(s.start).Truncate(time.Second)
				frame := spinnerFrames[i%len(spinnerFrames)]
				var line string
				if elapsed >= 2*time.Second {
					line = fmt.Sprintf("\r\033[K  %s %s (%s)", frame, s.msg, elapsed)
				} else {
					line = fmt.Sprintf("\r\033[K  %s %s", frame, s.msg)
				}
				s.mu.Unlock()
				write(line)
				i++
			}
		}
	}()
}

// Update changes the spinner message while it is running.
func (s *spinner) Update(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop halts the spinner and clears its line. It is safe to call
// multiple times (idempotent).
func (s *spinner) Stop() {
	s.mu.Lock()
	wasRunning := s.running
	isTTY := s.isTTY
	s.running = false
	if wasRunning {
		close(s.done)
	}
	s.mu.Unlock()

	if wasRunning {
		// Wait for the animation goroutine to fully exit so it can't
		// overwrite anything we print after this point.
		<-s.stopped
	}

	if isTTY && wasRunning {
		write("\r\033[K")
	}
}

// StopWith halts the spinner and prints one final status line.
func (s *spinner) StopWith(msg string) {
	s.mu.Lock()
	wasRunning := s.running
	isTTY := s.isTTY
	s.running = false
	if wasRunning {
		close(s.done)
	}
	s.mu.Unlock()

	if wasRunning {
		// Wait for the animation goroutine to fully exit so it can't
		// overwrite our final status line.
		<-s.stopped
	}

	if isTTY && wasRunning {
		write("\r\033[K")
	}
	write(fmt.Sprintf("  %s\n", msg))
}

// StopWithSuccess prints a ✓ prefixed message and stops the spinner.
func (s *spinner) StopWithSuccess(msg string) {
	s.StopWith("✓ " + msg)
}

// StopWithFailure prints a ✗ prefixed message and stops the spinner.
func (s *spinner) StopWithFailure(msg string) {
	s.StopWith("✗ " + msg)
}
