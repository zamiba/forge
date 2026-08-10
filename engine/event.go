package engine

// EventKind identifies what an Event reports.
type EventKind string

const (
	// EventRunStart fires once before the first step. Total is the number of
	// steps that will actually run (skipped conditional steps excluded).
	EventRunStart EventKind = "run:start"
	// EventStepStart fires before each executed step.
	EventStepStart EventKind = "step:start"
	// EventStepSkip fires for steps whose "if" condition evaluated false. These
	// do not advance Index and are not counted in Total.
	EventStepSkip EventKind = "step:skip"
	// EventStepDone fires after a step completes successfully.
	EventStepDone EventKind = "step:done"
	// EventProgress reports sub-step progress, currently only for fetch.
	EventProgress EventKind = "progress"
	// EventLog carries a single line of subprocess output.
	EventLog EventKind = "log"
	// EventRunDone fires once after the last step succeeds.
	EventRunDone EventKind = "run:done"
	// EventRunFailed fires once when a step fails or the run is cancelled.
	EventRunFailed EventKind = "run:failed"
)

// Event is a single progress report from a run. Fields not relevant to the Kind
// are zero.
type Event struct {
	Kind    EventKind `json:"kind"`
	Index   int       `json:"index,omitempty"`   // 0-based index of the running step
	Total   int       `json:"total,omitempty"`   // total steps that will run
	Step    string    `json:"step,omitempty"`    // step type, e.g. "fetch"
	Label   string    `json:"label,omitempty"`   // human-readable step description
	Phase   string    `json:"phase,omitempty"`   // for EventProgress
	Percent int       `json:"percent,omitempty"` // for EventProgress
	Line    string    `json:"line,omitempty"`    // for EventLog
	Stream  string    `json:"stream,omitempty"`  // "stdout" | "stderr", for EventLog
	Error   string    `json:"error,omitempty"`   // for EventRunFailed
}

// Handler receives events as a run proceeds. It is called synchronously from the
// goroutine driving the run, except for EventLog which is called from the
// goroutines draining a subprocess's pipes — implementations must be safe to
// call concurrently.
type Handler func(Event)
