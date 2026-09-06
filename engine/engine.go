package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Options configures a single run.
type Options struct {
	// Steps is the sequence to execute. Required.
	Steps []Step

	// Dependencies are the commands the spec declares it needs. They are
	// verified before the first step runs (unless SkipDependencyCheck is set),
	// and they double as the allowlist for the `run` step: a spec may only
	// invoke commands it has declared.
	Dependencies []string

	// Args are the resolved install argument values, substituted into step
	// fields as $name / ${name}. The names "platform" and "version" are
	// reserved and rejected here, since Run injects them itself.
	Args map[string]string

	// Platform is the target platform this run builds for, injected as
	// $platform. It is the axis that varies inside a single build — a spec
	// covering Linux and Windows branches on it with `if` — which is why it is
	// a variable rather than something the host bakes into the steps.
	Platform string

	// Version is the version being built, injected as $version.
	Version string

	// VersionOrder is every version the spec file declares, oldest first. It is
	// the hierarchy that ordered conditions such as "$version >= 1.1.0" resolve
	// against; see EvalCondition. Leave it empty when a spec uses no ordered
	// conditions.
	VersionOrder []string

	// RootDir is the working directory the first step starts in, and the base
	// that defineExecutable paths are recorded relative to. Required; created
	// if it does not exist.
	RootDir string

	// Providers back `copy` steps that declare a `from`. A step naming a
	// provider that is not registered fails the run.
	Providers map[string]Provider

	// StepHandlers registers additional step types, or overrides builtins.
	// This is how a host teaches the engine domain-specific operations without
	// widening what an untrusted spec can do.
	StepHandlers map[string]StepFunc

	// Events receives progress reports. Optional.
	Events Handler

	// Log receives raw subprocess output and a transcript of the run.
	// Optional.
	Log io.Writer

	// HTTPClient is used by fetch steps. Defaults to DefaultHTTPClient.
	HTTPClient *http.Client

	// SkipDependencyCheck omits the pre-flight PATH check but still populates
	// the `run` allowlist. Uninstall sequences use this: the tools that built
	// an item may be long gone by the time it is removed.
	SkipDependencyCheck bool

	// RequireExecutable fails the run when no defineExecutable step ran. Hosts
	// that install launchable software want this; hosts that use the engine to
	// produce an image or an archive do not.
	RequireExecutable bool

	// AllowPathEscape lets step paths resolve outside RootDir, via absolute
	// paths or "..". Leave it off for specs you do not control; turn it on for
	// hosts whose specs legitimately address the wider filesystem, such as one
	// managing subvolumes under a mount point.
	AllowPathEscape bool
}

// Result is what a successful run produced.
type Result struct {
	Executables []Executable
}

// StepFunc executes one step. Handlers registered in Options.StepHandlers
// receive the same State the builtins use.
type StepFunc func(ctx context.Context, st *State, step Step) error

// State is the mutable context of a run, and the API available to custom step
// handlers.
type State struct {
	// RootDir is where the run started. defineExecutable paths are relative
	// to it.
	RootDir string
	// WorkDir is the current working directory, moved by `cd` steps.
	WorkDir string
	// Args are the resolved install args.
	Args map[string]string

	executables []Executable
	providers   map[string]Provider
	allowed     map[string]bool
	allowEscape bool
	events      Handler
	log         io.Writer
	logMu       sync.Mutex
	client      *http.Client
	msys2Root   string
}

// Interp substitutes the run's args into s.
func (st *State) Interp(s string) string { return Interpolate(s, st.Args) }

// Resolve interpolates a step path and resolves it against the current working
// directory. Unless the host set AllowPathEscape, the result must stay inside
// RootDir: an untrusted spec should not be able to read or write outside the
// directory it was given, whether by an absolute path or by "..".
func (st *State) Resolve(path string) (string, error) {
	p := st.Interp(path)

	var full string
	if filepath.IsAbs(p) {
		full = filepath.Clean(p)
	} else {
		full = filepath.Clean(filepath.Join(st.WorkDir, p))
	}
	if st.allowEscape {
		return full, nil
	}

	root := filepath.Clean(st.RootDir)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves to %s, outside the run directory %s", path, full, root)
	}
	return full, nil
}

// Emit sends an event to the host, if one is listening.
func (st *State) Emit(e Event) {
	if st.events != nil {
		st.events(e)
	}
}

// Logf writes a line to the run log.
func (st *State) Logf(format string, v ...any) {
	if st.log == nil {
		return
	}
	st.logMu.Lock()
	defer st.logMu.Unlock()
	fmt.Fprintf(st.log, format+"\n", v...)
}

// AddExecutable records a launch target. Path should be relative to RootDir.
func (st *State) AddExecutable(e Executable) { st.executables = append(st.executables, e) }

// Command builds an *exec.Cmd for a subprocess: rooted at the current working
// directory, with env merged over the process environment and the MSYS2 bin
// directories prepended to PATH when the spec asked for msys2.
func (st *State) Command(ctx context.Context, name string, args []string, env map[string]string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = st.WorkDir
	cmd.Env = mergeEnv(env)
	if st.msys2Root != "" {
		cmd.Env = prependMSYS2Path(cmd.Env, st.msys2Root)
	}
	return cmd
}

// RunCommand runs cmd to completion, streaming its output to the run log and
// emitting it as log events line by line.
func (st *State) RunCommand(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", cmd.Path, err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); st.drain(stdout, "stdout") }()
	go func() { defer wg.Done(); st.drain(stderr, "stderr") }()
	wg.Wait()

	return cmd.Wait()
}

// drain forwards a subprocess pipe to the log and to log events. Lines longer
// than bufio's default cap are split rather than dropped, since build output
// can contain very long compiler invocations.
func (st *State) drain(r io.Reader, stream string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		st.Logf("%s", line)
		st.Emit(Event{Kind: EventLog, Line: line, Stream: stream})
	}
}

// resolveProvider asks the named provider for a source path.
func (st *State) resolveProvider(ctx context.Context, step Step) (string, error) {
	p, ok := st.providers[step.From]
	if !ok {
		return "", fmt.Errorf("no provider registered for from:%q", step.From)
	}
	return p.Resolve(ctx, ProviderRequest{
		From: step.From,
		Src:  st.Interp(step.Src),
		Args: st.Args,
		Step: step,
	})
}

// Run executes a step sequence and returns the executables it declared.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.RootDir == "" {
		return nil, fmt.Errorf("engine: RootDir is required")
	}
	if err := os.MkdirAll(opts.RootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}
	if !opts.SkipDependencyCheck && len(opts.Dependencies) > 0 {
		if err := CheckDependencies(opts.Dependencies); err != nil {
			return nil, err
		}
	}

	// Reserved names are injected into a copy. Mutating opts.Args would leak
	// them back to the host, and PortForge persists the arg map it passed in so
	// a rebuild can reuse it — writing $platform into that map would make the
	// next run fail the check below.
	for _, reserved := range []string{"platform", "version"} {
		if _, taken := opts.Args[reserved]; taken {
			return nil, fmt.Errorf("engine: arg %q is reserved and set by the engine", reserved)
		}
	}
	args := make(map[string]string, len(opts.Args)+2)
	for k, v := range opts.Args {
		args[k] = v
	}
	if opts.Platform != "" {
		args["platform"] = opts.Platform
	}
	if opts.Version != "" {
		args["version"] = opts.Version
	}
	client := opts.HTTPClient
	if client == nil {
		client = DefaultHTTPClient
	}

	// The declared dependencies are the allowlist for `run`.
	allowed := make(map[string]bool, len(opts.Dependencies))
	for _, d := range opts.Dependencies {
		allowed[d] = true
	}

	var msys2Root string
	if runtime.GOOS == "windows" && allowed[msys2Dep] {
		msys2Root = FindMSYS2()
	}

	st := &State{
		RootDir:     opts.RootDir,
		WorkDir:     opts.RootDir,
		Args:        args,
		providers:   opts.Providers,
		allowed:     allowed,
		allowEscape: opts.AllowPathEscape,
		events:      opts.Events,
		log:         opts.Log,
		client:      client,
		msys2Root:   msys2Root,
	}

	handlers := builtinSteps()
	for name, fn := range opts.StepHandlers {
		handlers[name] = fn
	}

	// Count the steps that will actually run so progress totals exclude the
	// ones their conditions skip.
	total := 0
	for _, s := range opts.Steps {
		if s.If == "" {
			total++
			continue
		}
		ok, err := EvalCondition(s.If, args, opts.VersionOrder)
		if err != nil {
			return nil, err
		}
		if ok {
			total++
		}
	}
	st.Emit(Event{Kind: EventRunStart, Total: total})

	fail := func(label, step string, index int, err error) (*Result, error) {
		st.Logf("[failed] %s: %v", label, err)
		st.Emit(Event{Kind: EventRunFailed, Index: index, Total: total, Step: step, Label: label, Error: err.Error()})
		return nil, err
	}

	index := 0
	for _, step := range opts.Steps {
		if step.If != "" {
			run, err := EvalCondition(step.If, args, opts.VersionOrder)
			if err != nil {
				return fail(StepLabel(step, args), step.Step, index, err)
			}
			if !run {
				label := StepLabel(step, args)
				st.Logf("[skipped] %s", label)
				st.Emit(Event{Kind: EventStepSkip, Step: step.Step, Label: label})
				continue
			}
		}

		if err := ctx.Err(); err != nil {
			return fail(StepLabel(step, args), step.Step, index, err)
		}

		label := StepLabel(step, args)
		st.Logf("[step %d/%d] %s", index+1, total, label)
		st.Emit(Event{Kind: EventStepStart, Index: index, Total: total, Step: step.Step, Label: label})

		handler, ok := handlers[step.Step]
		if !ok {
			return fail(label, step.Step, index, fmt.Errorf("unknown step type %q", step.Step))
		}
		if err := handler(ctx, st, step); err != nil {
			return fail(label, step.Step, index, fmt.Errorf("build step %q failed: %w", step.Step, err))
		}

		st.Emit(Event{Kind: EventStepDone, Index: index, Total: total, Step: step.Step, Label: label})
		index++
	}

	if opts.RequireExecutable && len(st.executables) == 0 {
		return fail("defineExecutable", "defineExecutable", index,
			fmt.Errorf("build produced no output: no 'defineExecutable' steps defined"))
	}

	st.Emit(Event{Kind: EventRunDone, Total: total})
	return &Result{Executables: st.executables}, nil
}

// StepLabel renders a short human-readable description of a step, with args
// substituted in so the label shows the paths the step will actually touch.
func StepLabel(step Step, args map[string]string) string {
	in := func(s string) string { return Interpolate(s, args) }
	// Copy Args rather than interpolating in place: the caller's step is reused
	// when the step actually executes.
	cmdArgs := make([]string, len(step.Args))
	for i, a := range step.Args {
		cmdArgs[i] = in(a)
	}
	step = Step{
		Step:       step.Step,
		From:       step.From,
		Src:        in(step.Src),
		Dest:       in(step.Dest),
		Cmd:        in(step.Cmd),
		Args:       cmdArgs,
		URL:        in(step.URL),
		Path:       in(step.Path),
		Executable: in(step.Executable),
	}
	switch step.Step {
	case "cd":
		return "cd " + step.Path
	case "fetch":
		return "fetch " + step.URL
	case "extract":
		return "extract " + step.Src
	case "copy":
		if step.From != "" {
			return fmt.Sprintf("copy %s:%s → %s", step.From, step.Src, step.Dest)
		}
		return "copy " + step.Src + " → " + step.Dest
	case "move":
		return "move " + step.Src + " → " + step.Dest
	case "make":
		return "make " + strings.Join(step.Args, " ")
	case "run":
		return strings.TrimSpace(step.Cmd + " " + strings.Join(step.Args, " "))
	case "createDir":
		return "createDir " + step.Path
	case "touch":
		return "touch " + step.Path
	case "deletePath":
		return "deletePath " + step.Path
	case "defineExecutable":
		return "defineExecutable " + step.Executable
	default:
		return step.Step
	}
}
