// Command forge runs install specs: a cross-platform, declarative alternative
// to a build shell script.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/zamiba/forge/engine"
)

const usage = `forge — run declarative install specs

Usage:
  forge run    [flags]   execute the matching spec
  forge args   [flags]   print the spec's user-configurable arguments as JSON
  forge check  [flags]   validate the spec and verify its dependencies
  forge steps            list the builtin step types

Run "forge <command> -h" for the flags of a command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:])
	case "args":
		err = cmdArgs(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "steps":
		for _, name := range engine.BuiltinStepNames() {
			fmt.Println(name)
		}
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "forge: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "forge: %v\n", err)
		os.Exit(1)
	}
}

// specFlags are the flags shared by every subcommand that reads a spec.
type specFlags struct {
	fs       *flag.FlagSet
	spec     *string
	dir      *string
	platform *string
	version  *string

	// Filled in by load().
	file     *engine.SpecFile
	order    []string
	resolved string
}

func newSpecFlags(name string) *specFlags {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	return &specFlags{
		fs:       fs,
		spec:     fs.String("spec", defaultSpecPath, "path to the spec file"),
		dir:      fs.String("dir", ".", "root directory the steps run in"),
		platform: fs.String("platform", engine.HostPlatform(), "platform to match against targetPlatforms"),
		version:  fs.String("version", "", "version to build (default: the file's defaultVersion, else the newest declared)"),
	}
}

// defaultSpecPath is the spec filename to look for when -spec is not given.
// The superseded name is still accepted so catalogs that have not been converted
// keep working.
const (
	defaultSpecPath = ".forge.json"
	legacySpecPath  = ".install.json"
)

// load parses the spec file and selects the build matching the platform and
// version. It also records the file's version order, which ordered `if`
// conditions resolve against, and the version actually being built.
func (f *specFlags) load() (*engine.Spec, error) {
	path := *f.spec
	if path == defaultSpecPath {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if _, err := os.Stat(legacySpecPath); err == nil {
				path = legacySpecPath
			}
		}
	}

	file, err := engine.LoadSpecFile(path)
	if err != nil {
		return nil, err
	}
	if file == nil || len(file.Specs) == 0 {
		return nil, fmt.Errorf("no builds found in %s", path)
	}
	f.file = file
	f.order = engine.VersionOrder(file.Specs)

	// A build can cover several versions, so an unspecified version is genuinely
	// ambiguous. Resolve it up front rather than letting Select pick whichever
	// build happens to come first.
	f.resolved = *f.version
	if f.resolved == "" {
		f.resolved = file.DefaultVersion
	}
	if f.resolved == "" && len(f.order) > 0 {
		f.resolved = f.order[len(f.order)-1] // newest: the order is oldest first
	}

	spec := engine.Select(file.Specs, *f.platform, f.resolved)
	if spec == nil {
		return nil, fmt.Errorf("no build in %s targets platform %q at version %q", path, *f.platform, f.resolved)
	}
	return spec, nil
}

// stringList collects repeated flags such as --arg k=v.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// parseKeyValues splits "k=v" pairs, keeping any "=" in the value.
func parseKeyValues(items []string, what string) (map[string]string, error) {
	out := make(map[string]string, len(items))
	for _, item := range items {
		k, v, ok := strings.Cut(item, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid %s %q: expected NAME=VALUE", what, item)
		}
		out[k] = v
	}
	return out, nil
}

func cmdRun(argv []string) error {
	f := newSpecFlags("run")
	var argValues, providerDirs stringList
	f.fs.Var(&argValues, "arg", "install argument as NAME=VALUE (repeatable)")
	f.fs.Var(&providerDirs, "provider", "back a copy step's `from` with a directory, as NAME=DIR (repeatable)")
	events := f.fs.String("events", "pretty", "event output: pretty, ndjson, or none")
	logPath := f.fs.String("log", "", "also write a plain-text transcript of the run to this file")
	skipDeps := f.fs.Bool("skip-deps", false, "skip the pre-flight dependency check")
	uninstall := f.fs.Bool("uninstall", false, "run uninstallSteps instead of steps")
	requireExe := f.fs.Bool("require-executable", false, "fail if no defineExecutable step ran")
	if err := f.fs.Parse(argv); err != nil {
		return err
	}

	spec, err := f.load()
	if err != nil {
		return err
	}

	supplied, err := parseKeyValues(argValues, "arg")
	if err != nil {
		return err
	}
	args, err := spec.ResolveArgs(supplied)
	if err != nil {
		return fmt.Errorf("%w\n\nRun \"forge args --spec %s\" to see the available arguments.", err, *f.spec)
	}

	providerRoots, err := parseKeyValues(providerDirs, "provider")
	if err != nil {
		return err
	}
	providers := make(map[string]engine.Provider, len(providerRoots))
	for name, root := range providerRoots {
		providers[name] = engine.DirProvider(root)
	}

	steps := spec.Steps
	if *uninstall {
		steps = spec.UninstallSteps
		if len(steps) == 0 {
			return errors.New("the spec defines no uninstallSteps")
		}
	}

	// Pretty output goes to stderr so stdout stays clean for NDJSON consumers.
	handler, closeEvents, err := eventHandler(*events)
	if err != nil {
		return err
	}
	defer closeEvents()

	logw, closeLog, err := logWriter(*logPath)
	if err != nil {
		return err
	}
	defer closeLog()

	root, err := filepath.Abs(*f.dir)
	if err != nil {
		return err
	}

	// Ctrl-C cancels the run at the next step boundary and kills any running
	// subprocess, matching what a host application's cancel button does.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The spec-derived half of Options comes from the spec rather than being
	// retyped here, so a field added to it reaches this caller too. Uninstalling
	// takes the other constructor: it selects the teardown sequence, skips the
	// dependency check, and stops the engine from setting user data aside, which
	// is an install's protection rather than a removal's.
	var opts engine.Options
	if *uninstall {
		opts = spec.TeardownOptions(*f.platform, f.resolved, args, f.order)
	} else {
		opts = spec.BuildOptions(*f.platform, f.resolved, args, f.order)
	}
	opts.RootDir = root
	opts.Providers = providers
	opts.Events = handler
	opts.Log = logw
	opts.RequireExecutable = *requireExe
	if *skipDeps {
		opts.SkipDependencyCheck = true
	}

	res, err := engine.Run(ctx, opts)
	if err != nil {
		return err
	}

	// The executables are the run's actual result, so they go to stdout even in
	// pretty mode where progress went to stderr.
	if len(res.Executables) > 0 {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res.Executables)
	}
	return nil
}

func cmdArgs(argv []string) error {
	f := newSpecFlags("args")
	if err := f.fs.Parse(argv); err != nil {
		return err
	}
	spec, err := f.load()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	prompts := spec.Prompts()
	if prompts == nil {
		prompts = []engine.Prompt{}
	}
	return enc.Encode(prompts)
}

func cmdCheck(argv []string) error {
	f := newSpecFlags("check")
	if err := f.fs.Parse(argv); err != nil {
		return err
	}
	spec, err := f.load()
	if err != nil {
		return err
	}

	builtin := map[string]bool{}
	for _, name := range engine.BuiltinStepNames() {
		builtin[name] = true
	}
	declared := map[string]bool{}
	for _, d := range spec.Dependencies {
		declared[d] = true
	}

	var problems []string
	checkSteps := func(steps []engine.Step, kind string) {
		for i, s := range steps {
			if !builtin[s.Step] {
				problems = append(problems, fmt.Sprintf("%s[%d]: unknown step type %q", kind, i, s.Step))
			}
			if s.Step == "run" && s.Cmd != "" && !declared[s.Cmd] {
				problems = append(problems, fmt.Sprintf("%s[%d]: run %q is not declared in dependencies", kind, i, s.Cmd))
			}
		}
	}
	checkSteps(spec.Steps, "steps")
	checkSteps(spec.UninstallSteps, "uninstallSteps")

	// A $name nothing declares is left in the string verbatim at run time, so
	// the build gets as far as the step carrying it and then fails with the
	// invoked tool's complaint rather than anything naming the spec. Catching it
	// here is the whole point of a check command.
	for _, name := range engine.UndeclaredArgs(spec) {
		problems = append(problems, fmt.Sprintf("${%s} is used but nothing declares it", name))
	}

	// Unbraced references are not interpolated at all, so they fail by doing
	// nothing rather than by failing. Naming the replacement matters more than
	// naming the problem here, since every one of these is a spec written
	// before braces became mandatory.
	for _, name := range engine.UnbracedRefs(spec) {
		problems = append(problems, fmt.Sprintf("$%s is not interpolated — write ${%s}, or ${args.%s} for an argument", name, name, name))
	}

	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problem(s) found in %s", len(problems), *f.spec)
	}

	if err := engine.CheckDependencies(spec.Dependencies); err != nil {
		return err
	}
	fmt.Printf("%s: ok (%d steps, %d dependencies)\n", *f.spec, len(spec.Steps), len(spec.Dependencies))
	return nil
}

// eventHandler builds the event sink for the chosen output mode.
func eventHandler(mode string) (engine.Handler, func(), error) {
	switch mode {
	case "none":
		return nil, func() {}, nil

	case "ndjson":
		enc := json.NewEncoder(os.Stdout)
		return func(e engine.Event) { _ = enc.Encode(e) }, func() {}, nil

	case "pretty":
		return func(e engine.Event) {
			switch e.Kind {
			case engine.EventStepStart:
				fmt.Fprintf(os.Stderr, "[%d/%d] %s\n", e.Index+1, e.Total, e.Label)
			case engine.EventStepSkip:
				fmt.Fprintf(os.Stderr, "  ... skipped: %s\n", e.Label)
			case engine.EventLog:
				fmt.Fprintf(os.Stderr, "  | %s\n", e.Line)
			case engine.EventProgress:
				fmt.Fprintf(os.Stderr, "\r  %s %d%%", e.Phase, e.Percent)
				if e.Percent >= 100 {
					fmt.Fprintln(os.Stderr)
				}
			case engine.EventRunDone:
				fmt.Fprintln(os.Stderr, "done")
			case engine.EventRunFailed:
				fmt.Fprintf(os.Stderr, "failed at %s: %s\n", e.Label, e.Error)
			}
		}, func() {}, nil

	default:
		return nil, nil, fmt.Errorf("invalid --events value %q: want pretty, ndjson, or none", mode)
	}
}

// logWriter opens the transcript file named by --log. Without one there is no
// transcript: both output modes already surface subprocess output as log events,
// so writing it to stderr as well would double every line.
func logWriter(path string) (io.Writer, func(), error) {
	if path == "" {
		return nil, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}
