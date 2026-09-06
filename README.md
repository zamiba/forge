# Forge

A cross-platform, declarative alternative to a build shell script.

Forge executes an **install spec** — a JSON file describing an ordered sequence of steps that fetch, unpack, build, and lay out software. It exists because "download this archive, extract it, drop a data file in, run make, move the result into place" is the same job on every platform, but writing it as a shell script means writing it three times and shipping an interpreter to run it.

It ships as a Go library and a CLI over the same code. The library is what applications embed; the CLI is for authoring specs, testing them, and driving them from programs that aren't written in Go.

```bash
go install github.com/zamiba/forge/cmd/forge@latest
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
| `--spec` | `.forge.json` | Path to the spec file. Falls back to `.install.json` when the default is used and no `.forge.json` exists. |
| `--dir` | `.` | Root directory the steps run in. |
| `--platform` | host OS | Platform to build for, matched against `targetPlatforms`. Injected as `$platform`. |
| `--version` | see below | Version to build. Defaults to the file's `defaultVersion`, else the newest declared. Injected as `$version`. |
| `--arg NAME=VALUE` | — | Supply an install argument. Repeatable. |
| `--provider NAME=DIR` | — | Back a `copy` step's `from` with a directory. Repeatable. |
| `--events` | `pretty` | `pretty`, `ndjson`, or `none`. |
| `--log FILE` | — | Also write a plain-text transcript of the run. |
| `--uninstall` | off | Run `uninstallSteps` instead of `steps`. |
| `--require-executable` | off | Fail if no `defineExecutable` step ran. |

```bash
forge check --spec .forge.json
forge run   --spec .forge.json --dir ~/games/mygame --arg region=us
forge run   --spec .forge.json --dir ./out --events ndjson | jq -c 'select(.kind=="step:start")'
```

`pretty` progress goes to stderr, so stdout carries only the run's result — the JSON array of declared executables — and stays pipeable.

---

## Spec format

A spec file declares **builds**. Each build states the versions and platforms it
covers and the steps that produce them; `Select` returns the first build matching
the requested platform and version.

The file takes either of two forms. The short one is a bare JSON array of builds.
The other is an object whose top level carries settings shared across builds:

```json
{
  "defaultVersion": "1.0.0",
  "dependencies": ["make", "gcc", "unzip"],
  "builds": [
    { "versions": ["1.0.0"], "targetPlatforms": ["Linux"], "steps": [] },
    { "versions": ["2.0.0"], "dependencies": ["cmake"], "steps": [] }
  ]
}
```

A header value is a **default**: a build that declares the same field replaces it
outright rather than merging with it, and `"dependencies": []` on a build means it
needs none rather than that it inherits. Only the declarative fields —
`dependencies`, `args`, `buildPaths`, `uninstallSteps` — can be defaulted this way.
A build's `steps` are always written out in full, because merging two sequences has
no obvious meaning and every scheme for it makes specs harder to read than the
duplication does.

A full build:

```json
[{
  "versions": ["1.0.0"],
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
| `versions` | string[] | The versions this build produces, oldest first. |
| `version` | string | Superseded scalar form of `versions`, still read. |
| `targetPlatforms` | string[] | `"Linux"`, `"Mac"`, `"Windows"`. Omit to match every platform. |
| `dependencies` | string[] | Commands that must be on `PATH`. Also the allowlist for `run` — see below. |
| `args` | object | User-configurable parameters. |
| `steps` | object[] | The ordered build sequence. |
| `uninstallSteps` | object[] | Optional teardown sequence. |
| `buildPaths` | string[] | Directories a host may delete to clean up after a failed run. |

File-level only:

| Field | Type | Description |
| --- | --- | --- |
| `defaultVersion` | string | The version a host should offer first. Must be one a build declares. |
| `builds` | object[] | The builds themselves. Required in the object form. |

### Versions

Declaration order across the file is **chronological, oldest first**, and it is the
version hierarchy that ordered conditions compare against. Preference is stated
separately with `defaultVersion`, because the newest release is not always the one
to recommend — a project whose newest build is a release candidate names the stable
one there instead.

A build covering several versions *and* several platforms asserts that every
combination of the two is buildable. A version that ships for fewer platforms than
its siblings therefore needs a build of its own; folding it in would advertise a
build that does not exist. Nothing about the JSON looks wrong when this is
violated, so it is worth checking deliberately.

### Args

Each arg is `choice` (a fixed set of values) or `string` (free text), and gets a `label` for the host to display. A `default` is used when the host supplies no value; a `choice` arg with neither a value nor a default is an error rather than a silent empty string.

Any string field in a step supports `$name` and `${name}` substitution. Use the braces when the name is followed by more characters: `${region}_pc`. A name with no matching arg is left as written rather than blanked, so a typo shows up as a visibly wrong path instead of a truncated one.

That is the right behaviour once a build is running and a poor one before it starts: nothing fails until the step carrying the reference executes, and what surfaces then is the invoked tool's complaint about a nonsensical argument rather than anything naming the spec. **`forge check` reports every `$name` that no arg declares**, which is where a mistake like this should be caught. It is a check rather than a run-time error because a literal dollar sign is not always a mistake — a step may pass one to a tool with its own idea of what it means.

### Conditional steps

Any step may carry an `if`, evaluated after interpolation. Skipped steps are
excluded from progress totals.

`==` and `!=` compare as strings. A bare value is true when it is non-empty and not
`false` or `0`.

```json
{ "step": "fetch", "if": "${textureMod} != none", "url": "…", "dest": ".build/textures.7z" }
```

`>`, `>=`, `<` and `<=` compare **positions in the version hierarchy**, not parsed
version numbers:

```json
{ "step": "run", "if": "$version >= 1.1.0", "cmd": "./migrate-config.sh" }
```

Nothing here tries to understand a version string, because real ones do not support
it — a catalog holding `Barnard Alfa`, `1.1 RC4` and `Deckard Alfa (1.0.0)` has no
parseable ordering, and a parser that guessed one would fail silently. Both operands
must be versions the file declares; anything else is an error, so a mistyped
threshold stops the run instead of quietly disabling a step.

### Reserved arguments

Two names are set by the engine and rejected if a caller supplies them:

| Name | Value |
| --- | --- |
| `$platform` | The platform this run is building for. |
| `$version` | The version being built. |

`$platform` is the axis that varies *inside* a single build, which is what makes it
worth branching on: one build covering Linux and Windows writes the handful of steps
that differ with an `if`, and shares the rest.

Reach for it in conditions rather than to assemble upstream names. Writing
`"url": ".../${platform}.zip"` bets that a project names its artifacts to match, and
that bet is lost the first time a release tags `1.1-rc4` for a version called
`1.1 RC4`. Prefer literal URLs branched with `if`, and normalise the local filenames
you control so the rest of the pipeline stays shared:

```json
{ "step": "fetch", "if": "$platform == Windows",
  "url": "https://example.com/releases/1.0.2/Windows.zip", "dest": ".build/package.zip" },
{ "step": "fetch", "if": "$platform != Windows",
  "url": "https://example.com/releases/1.0.2/Linux.zip",   "dest": ".build/package.zip" }
```

That also means every URL a spec will ever fetch can be found by reading it.

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

**Variables must be declared.** `forge check` fails on any `$name` a step interpolates that no `args` entry declares, `$platform` and `$version` excepted.

**Commands must be declared.** `run` only executes a command named in `dependencies`. Arguments are passed to the process directly and never through a shell, so `cmd` cannot smuggle in a pipeline or a second command. `forge check` reports any `run` whose command is undeclared, before the spec ever executes.

**Paths stay inside the run directory.** Every step path resolves under `--dir` and is rejected if it escapes, whether by an absolute path or by `..`. Archive entries are checked the same way, so a crafted archive can't write outside the destination either. Hosts that legitimately need the wider filesystem set `AllowPathEscape`.

**Outside content arrives through providers.** A `copy` step naming a `from` asks the host to resolve it. The engine never guesses where a provider's content lives, and a spec cannot reach content the host hasn't offered.

What is *not* bounded: the URLs a spec fetches, and what a declared command does once running. `make` runs a Makefile, and a Makefile can do anything. Declaring a dependency is a decision to trust it. Checksum pinning for `fetch` is the obvious next hardening step and is not implemented yet.

---

## Library use

```go
import "github.com/zamiba/forge/engine"

file, _ := engine.LoadSpecFile(".forge.json")
order := engine.VersionOrder(file.Specs)
version := file.DefaultVersion
spec := engine.Select(file.Specs, engine.HostPlatform(), version)
args, _ := spec.ResolveArgs(map[string]string{"region": "us"})

// What `forge check` reports. A host syncing specs it did not write can run
// this before building, and fail with the name of the offending variable
// rather than six steps later with a compiler's error message.
if missing := engine.UndeclaredArgs(spec); len(missing) > 0 {
    return fmt.Errorf("spec uses undeclared args: %v", missing)
}

res, err := engine.Run(ctx, engine.Options{
    Steps:        spec.Steps,
    Dependencies: spec.Dependencies,
    Args:         args,
    Platform:     engine.HostPlatform(),
    Version:      version,
    VersionOrder: order,
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
