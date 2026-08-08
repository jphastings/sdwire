package sdwire

import "time"

// PowerFunc controls power delivery to the target board (the Device Under
// Test) — not to the SDWire itself. Implementations should block until the
// requested power state has taken effect.
type PowerFunc func(shouldBeOn bool) error

// DefaultMinDarkTime is the minimum time PowerCycle keeps the target
// board's power off when no explicit minimum is given. Small DUT boards can
// ride through a mains interruption of a couple of seconds on PSU bulk
// capacitance without actually resetting, so a genuine power cycle needs a
// longer guaranteed dark time.
const DefaultMinDarkTime = 8 * time.Second

var sleep = time.Sleep

// SetTargetPower configures the function used to control power to the
// target board. Pass nil to remove any previously configured PowerFunc.
func (s *SDWire) SetTargetPower(fn PowerFunc) {
	s.powerFunc = fn
}

// HasTargetPower reports whether a PowerFunc has been configured.
func (s *SDWire) HasTargetPower() bool {
	return s.powerFunc != nil
}

// TargetPower turns power to the target board on or off. If no PowerFunc
// has been configured, this is a documented no-op that returns nil.
func (s *SDWire) TargetPower(on bool) error {
	if s.powerFunc == nil {
		return nil
	}
	return s.powerFunc(on)
}

// PowerCycle power-cycles the target board: power off, a guaranteed dark
// time, then power on. minOff sets the minimum time power stays off; values
// <= 0 fall back to DefaultMinDarkTime. PowerCycle never sleeps for less
// than the requested dark time. If no PowerFunc is configured, PowerCycle is
// a no-op that returns nil without sleeping.
func (s *SDWire) PowerCycle(minOff time.Duration) error {
	if s.powerFunc == nil {
		return nil
	}
	if minOff <= 0 {
		minOff = DefaultMinDarkTime
	}
	if err := s.powerFunc(false); err != nil {
		return err
	}
	sleep(minOff)
	return s.powerFunc(true)
}
