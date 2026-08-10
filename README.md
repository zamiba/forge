# Forge

A cross-platform, declarative alternative to a build shell script.

Forge executes an **install spec** — a JSON file describing an ordered sequence of steps that fetch, unpack, build, and lay out software. It exists because "download this archive, extract it, drop a data file in, run make, move the result into place" is the same job on every platform, but writing it as a shell script means writing it three times and shipping an interpreter to run it.

It ships as a Go library and a CLI over the same code. The library is what applications embed; the CLI is for authoring specs, testing them, and driving them from programs that aren't written in Go.

```bash
go install github.com/portforge/forge/cmd/forge@latest
```

---

## Why not a shell script

A spec is data, not code. That buys three things a script can't give you:

- **It runs the same everywhere.** No bash on Windows, no `cp -r` vs `robocopy`, no quoting differences. Archive extraction, recursive copies, atomic moves and HTTP downloads are engine builtins, not shell-outs.
- **You can read what it will do.** Every command a spec can invoke must appear in its `dependencies` array. Reading that array tells you the complete set of executables the spec can run — before you run it.
- **The host application can see inside it.** Progress, step boundaries, and subprocess output arrive as structured events rather than as text on a terminal, so a GUI can render a progress bar and a live build log without parsing anything.

---

## The CLI

```bash
forge run    [flags]   # execute the matching spec
forge args   [flags]   # print the spec's user-configurable arguments as JSON
forge check  [flags]   # validate the spec and verify its dependencies
forge steps            # list the builtin step types
```

Common flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--spec` | `.install.json` | Path to the spec file. |
| `--dir` | `.` | Root directory the steps run in. |
| `--platform` | host OS | Platform to match against `targetPlatforms`. |
| `--arg NAME=VALUE` | — | Supply an install argument. Repeatable. |
| `--provider NAME=DIR` | — | Back a `copy` step's `from` with a directory. Repeatable. |
| `--events` | `pretty` | `pretty`, `ndjson`, or `none`. |
| `--log FILE` | — | Also write a plain-text transcript of the run. |
| `--uninstall` | off | Run `uninstallSteps` instead of `steps`. |
| `--require-executable` | off | Fail if no `defineExecutable` step ran. |

```bash
forge check --spec .install.json
forge run   --spec .install.json --dir ~/games/mygame --arg region=us
forge run   --spec .install.json --dir ./out --events ndjson | jq -c 'select(.kind=="step:start")'
```

`pretty` progress goes to stderr, so stdout carries only the run's result — the JSON array of declared executables — and stays pipeable.

---

## Spec format

A spec file is a JSON **array** of specs. The first whose `targetPlatforms` matches the host is the one that runs.

```json
[{
  "version": "1.0.0",
  "targetPlatforms": ["Linux"],
  "dependencies": ["make", "gcc", "unzip"],
  "args": {
    "region": {
      "type": "choice",
      "label": "ROM region",
      "default": "us",
      "options": [
        { "value": "us", "label": "US (NTSC)" },
        { "value": "eu", "label": "European (PAL)" }
      ]
    }
  },
  "steps": [
    { "step": "createDir", "path": ".build" },
    { "step": "fetch", "url": "https://example.com/source.zip", "dest": ".build/source.zip" },
    { "step": "extract", "src": ".build/source.zip", "dest": ".build" },
    { "step": "copy", "from": "rom", "src": "Super Mario 64 (USA)", "dest": ".build/baserom.${region}.z64" },
    { "step": "run", "cmd": "make", "args": ["VERSION=${region}"] },
    { "step": "move", "src": ".build/build/${region}_pc", "dest": "install" },
    { "step": "deletePath", "path": ".build" },
    { "step": "defineExecutable", "executable": "install/sm64.${region}.f3dex2e", "title": "Play" }
  ],
  "uninstallSteps": [
    { "step": "deletePath", "path": "install" }
  ]
}]
```

| Field | Type | Description |
| --- | --- | --- |
| `version` | string | Spec version, recorded by hosts that track what they installed. |
| `targetPlatforms` | string[] | `"Linux"`, `"Mac"`, `"Windows"`. Omit to match every platform. |
| `dependencies` | string[] | Commands that must be on `PATH`. Also the allowlist for `run` — see below. |
| `args` | object | User-configurable parameters. |
| `steps` | object[] | The ordered build sequence. |
| `uninstallSteps` | object[] | Optional teardown sequence. |
| `buildPaths` | string[] | Directories a host may delete to clean up after a failed run. |

### Args

Each arg is `choice` (a fixed set of values) or `string` (free text), and gets a `label` for the host to display. A `default` is used when the host supplies no value; a `choice` arg with neither a value nor a default is an error rather than a silent empty string.

Any string field in a step supports `$name` and `${name}` substitution. Use the braces when the name is followed by more characters: `${region}_pc`. A name with no matching arg is left as written rather than blanked, so a typo shows up as a visibly wrong path instead of a truncated one.

### Conditional steps

Any step may carry an `if`, evaluated after interpolation. It supports `==`, `!=`, and a bare value that is true when non-empty and not `false` or `0`. Skipped steps are excluded from progress totals.

```json
{ "step": "fetch", "if": "${textureMod} != none", "url": "…", "dest": ".build/textures.7z" }
```

---

## Steps

Paths are relative to the current working directory, which starts at the run's root and moves with `cd`.

| Step | Fields | Description |
| --- | --- | --- |
| `cd` | `path` | Change the working directory for subsequent steps. |
| `fetch` | `url`, `dest` | Download a file. Written to a temp file and renamed into place, so an interrupted download can't leave a truncated file behind. |
| `extract` | `src`, `dest` | Unpack `.zip`, `.7z`, `.tar.gz`/`.tgz`. Format inferred from the extension; permissions preserved. |
| `copy` | `src`, `dest`, or `from` | Copy a file or directory recursively. With `from`, the source comes from a registered provider. |
| `move` | `src`, `dest` | Atomic rename where possible, falling back to copy + delete across filesystems. |
| `run` | `cmd`, `args`, `env` | Run a declared dependency. |
| `make` | `args`, `env` | Run `make`. Predates `run` and kept for compatibility. |
| `createDir` | `path` | `mkdir -p`. |
| `touch` | `path` | Create an empty file, including parent directories. Leaves an existing file's contents alone. |
| `deletePath` | `path` | `rm -rf`. Succeeds when the path is already absent. |
| `defineExecutable` | `executable`, `title` | Record a launch target. Applies `chmod +x` on non-Windows. Moves no files. |

Multiple `defineExecutable` steps are allowed; hosts typically treat the first as the default.

---

## What a spec can and cannot do

Forge runs specs you may not have written — a catalog of them can be synced from the internet. Three rules bound what one can do:

**Commands must be declared.** `run` only executes a command named in `dependencies`. Arguments are passed to the process directly and never through a shell, so `cmd` cannot smuggle in a pipeline or a second command. `forge check` reports any `run` whose command is undeclared, before the spec ever executes.

**Paths stay inside the run directory.** Every step path resolves under `--dir` and is rejected if it escapes, whether by an absolute path or by `..`. Archive entries are checked the same way, so a crafted archive can't write outside the destination either. Hosts that legitimately need the wider filesystem set `AllowPathEscape`.

**Outside content arrives through providers.** A `copy` step naming a `from` asks the host to resolve it. The engine never guesses where a provider's content lives, and a spec cannot reach content the host hasn't offered.

What is *not* bounded: the URLs a spec fetches, and what a declared command does once running. `make` runs a Makefile, and a Makefile can do anything. Declaring a dependency is a decision to trust it. Checksum pinning for `fetch` is the obvious next hardening step and is not implemented yet.

---

## Library use

```go
import "github.com/portforge/forge/engine"

specs, _ := engine.LoadSpecFile(".install.json")
spec := engine.Select(specs, engine.HostPlatform())
args, _ := spec.ResolveArgs(map[string]string{"region": "us"})

res, err := engine.Run(ctx, engine.Options{
    Steps:        spec.Steps,
    Dependencies: spec.Dependencies,
    Args:         args,
    RootDir:      installDir,
    Providers:    map[string]engine.Provider{"rom": myRomLibrary},
    Events:       func(e engine.Event) { ui.Report(e) },
    Log:          logFile,
})
```

`Run` blocks until the sequence finishes and honours context cancellation at step boundaries, killing any running subprocess.

### Providers

A provider resolves a `copy` step's `from` to a local path — the seam between the engine and wherever a host keeps its content:

```go
type Provider interface {
    Resolve(ctx context.Context, req ProviderRequest) (string, error)
}
```

PortForge registers a `rom` provider that matches `req.Src` against a game's declared ROM dependencies and returns the matching file from the user's library. `engine.DirProvider(root)` resolves `req.Src` beneath a directory and backs the CLI's `--provider` flag.

### Custom steps

`StepHandlers` adds step types or overrides builtins, giving a host domain-specific operations without widening what an untrusted spec can do:

```go
engine.Options{
    StepHandlers: map[string]engine.StepFunc{
        "subvolume": func(ctx context.Context, st *engine.State, s engine.Step) error {
            path, err := st.Resolve(s.Path)
            if err != nil {
                return err
            }
            return st.RunCommand(st.Command(ctx, "btrfs", []string{"subvolume", "create", path}, nil))
        },
    },
}
```

Handlers get `State`, which carries the working directory, the args, `Interp`/`Resolve` for paths, `Emit`/`Logf` for reporting, and `Command`/`RunCommand` for subprocesses. Fields outside the builtin `Step` struct are read with `step.DecodeRaw(&myStruct)`.

### Events

| Kind | Meaning |
| --- | --- |
| `run:start` | Emitted once; `Total` is the number of steps that will run. |
| `step:start` / `step:done` | Step boundaries, with `Index`, `Total`, `Step` and `Label`. |
| `step:skip` | A step whose `if` evaluated false. Not counted in `Total`. |
| `progress` | Sub-step progress; currently `fetch` downloads. |
| `log` | One line of subprocess output, tagged `stdout` or `stderr`. |
| `run:done` / `run:failed` | Terminal. `run:failed` carries the failing step and the error. |

`log` events arrive from the goroutines draining a subprocess's pipes, so a handler must be safe to call concurrently. Every other kind is emitted from the goroutine driving the run.

---

## Licence

GPL-3.0, matching PortForge.
