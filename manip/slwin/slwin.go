// Package slwin provides a transparent sliding-window AIL manipulation.
package slwin

import (
	"strconv"
	"strings"

	"github.com/neutrome-labs/ail"
	"github.com/neutrome-labs/ail/manip"
)

const (
	// DefaultKeepStart is the number of leading messages kept by default.
	DefaultKeepStart = 1

	// DefaultKeepEnd is the number of trailing messages kept by default.
	DefaultKeepEnd = 10
)

// SlidingWindow keeps a fixed-size window of messages.
//
// Messages outside the window are removed entirely. Non-message instructions
// such as SET_MODEL, tool definitions, and provider extensions are preserved.
type SlidingWindow struct {
	KeepStart int
	KeepEnd   int
}

// Option configures a SlidingWindow.
type Option = manip.Option[SlidingWindow]

// WithKeepStart sets how many leading messages are preserved.
func WithKeepStart(n int) Option {
	return func(s *SlidingWindow) {
		if n >= 0 {
			s.KeepStart = n
		}
	}
}

// WithKeepEnd sets how many trailing messages are preserved.
func WithKeepEnd(n int) Option {
	return func(s *SlidingWindow) {
		if n > 0 {
			s.KeepEnd = n
		}
	}
}

// New creates a SlidingWindow with router-compatible defaults.
func New(opts ...Option) *SlidingWindow {
	s := &SlidingWindow{
		KeepStart: DefaultKeepStart,
		KeepEnd:   DefaultKeepEnd,
	}
	manip.ApplyOptions(s, opts...)
	return s
}

// FromParams creates a SlidingWindow from the router plugin parameter syntax:
//
//	slwin          -> keep 1 from start, 10 from end
//	slwin:15       -> keep 1 from start, 15 from end
//	slwin:15:3     -> keep 3 from start, 15 from end
func FromParams(params string) *SlidingWindow {
	s := New()
	if params == "" {
		return s
	}

	parts := strings.SplitN(params, ":", 2)
	if v, err := strconv.Atoi(parts[0]); err == nil && v > 0 {
		s.KeepEnd = v
	}
	if len(parts) == 2 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v >= 0 {
			s.KeepStart = v
		}
	}
	return s
}

// Apply applies the sliding-window transform to prog.
func (s *SlidingWindow) Apply(prog *ail.Program) (*ail.Program, error) {
	if prog == nil {
		return nil, nil
	}
	if s == nil {
		return prog, nil
	}
	return Apply(prog, s.KeepEnd, s.KeepStart), nil
}

// Apply returns a copy of prog with only the requested leading and trailing
// messages retained. If the window covers all messages, prog is returned
// unchanged.
func Apply(prog *ail.Program, keepEnd, keepStart int) *ail.Program {
	if prog == nil {
		return nil
	}
	if keepEnd <= 0 {
		keepEnd = DefaultKeepEnd
	}
	if keepStart < 0 {
		keepStart = DefaultKeepStart
	}

	msgs := prog.Messages()
	total := len(msgs)
	if total <= keepStart+keepEnd {
		return prog
	}

	keepSet := make(map[int]bool, keepStart+keepEnd)
	for i := 0; i < keepStart && i < total; i++ {
		keepSet[i] = true
	}
	for i := total - keepEnd; i < total; i++ {
		if i >= 0 {
			keepSet[i] = true
		}
	}

	var toRemove []ail.MessageSpan
	for i, msg := range msgs {
		if !keepSet[i] {
			toRemove = append(toRemove, msg)
		}
	}
	if len(toRemove) == 0 {
		return prog
	}
	return prog.RemoveMessages(toRemove...)
}

var _ manip.Manip = (*SlidingWindow)(nil)
