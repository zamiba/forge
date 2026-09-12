# Forge

A cross-platform, declarative alternative to a build shell script.

Forge executes an **install spec** — a JSON file describing an ordered sequence of steps that fetch, unpack, build, and lay out software. It exists because "download this archive, extract it, drop a data file in, run make, move the result into place" is the same job on every platform, but writing it as a shell script means writing it three times and shipping an interpreter to run it.

It ships as a Go library and a CLI over the same code. The library is what applications embed; the CLI is for authoring specs, testing them, and driving them from programs that aren't written in Go.

Throughout, the **host** is whatever program calls the engine — PortForge, the `forge` CLI itself, or anything else embedding the library. The distinction matters because forge deliberately knows nothing about games, ROMs, or where content lives: the host supplies all of that through the seams described under [Library use](#library-use).

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
| `--platform` | host OS | Platform to build for, matched against `targetPlatforms`. Injected as `${platform}`. |
| `--version` | see below | Version to build. Defaults to the file's `defaultVersion`, else the newest declared. Injected as `${version}`. |
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
`dependencies`, `args`, `buildPaths` — can be defaulted this way. `uninstallSteps` and
`userDataPaths` are file-level *only*: see [User data](#user-data).
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
    { "step": "copy", "from": "rom", "src": "Super Mario 64 (USA)", "dest": ".build/baserom.${args.region}.z64" },
    { "step": "run", "cmd": "make", "args": ["VERSION=${args.region}"] },
    { "step": "move", "src": ".build/build/${args.region}_pc", "dest": "install" },
    { "step": "deletePath", "path": ".build" },
    { "step": "defineExecutable", "executable": "install/sm64.${args.region}.f3dex2e", "title": "Play" }
  ]
}]
```

| Field | Type | Description |
| --- | --- | --- |
| `versions` | string[] \| object | The versions this build produces, oldest first. The object form binds variables per version. |
| `version` | string | Superseded scalar form of `versions`, still read. |
| `targetPlatforms` | string[] \| object | `"Linux"`, `"Mac"`, `"Windows"`. Omit to match every platform. The object form binds variables per platform. |
| `dependencies` | string[] | Commands that must be on `PATH`. Also the allowlist for `run` — see below. |
| `args` | object | User-configurable parameters. |
| `steps` | object[] | The ordered build sequence. |
| `buildPaths` | string[] | Directories a host may delete to clean up after a failed run. |

File-level only:

| Field | Type | Description |
| --- | --- | --- |
| `defaultVersion` | string | The version a host should offer first. Must be one a build declares. |
| `uninstallSteps` | object[] | Optional teardown sequence, shared by every build. |
| `userDataPaths` | string[] | Paths a `deletePath` must leave behind — see [User data](#user-data). |
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

Any string field in a step supports `${...}` substitution. **Braces are required**, and every reference is namespaced:

| Reference | Resolves to |
| --- | --- |
| `${args.region}` | A user-configurable argument declared in `args`. |
| `${platform}` | The platform this run is building for. |
| `${version}` | The version being built. |
| `${platform.slug}` | A variable the selected platform binds — see [Platform and version variables](#platform-and-version-variables). |
| `${version.tag}` | A variable the selected version binds. |

A name with no matching value is left as written rather than blanked, so a typo shows up as a visibly wrong path instead of a truncated one.

That is the right behaviour once a build is running and a poor one before it starts: nothing fails until the step carrying the reference executes, and what surfaces then is the invoked tool's complaint about a nonsensical argument rather than anything naming the spec. **`forge check` reports every `${name}` that resolves to nothing**, which is where a mistake like this should be caught. It is a check rather than a run-time error because a literal dollar sign is not always a mistake — a step may pass one to a tool with its own idea of what it means.

It also reports the superseded unbraced form. `$name` is no longer substituted at all, which fails in the quietest way available: `"if": "$platform == Windows"` compares the literal text against `Windows`, is false forever, and skips its step without a word.

### Conditional steps

Any step may carry an `if`, evaluated after interpolation. Skipped steps are
excluded from progress totals.

`==` and `!=` compare as strings. A bare value is true when it is non-empty and not
`false` or `0`.

```json
{ "step": "fetch", "if": "${args.textureMod} != none", "url": "…", "dest": ".build/textures.7z" }
```

`>`, `>=`, `<` and `<=` compare **positions in the version hierarchy**, not parsed
version numbers:

```json
{ "step": "run", "if": "${version} >= 1.1.0", "cmd": "./migrate-config.sh" }
```

Nothing here tries to understand a version string, because real ones do not support
it — a catalog holding `Barnard Alfa`, `1.1 RC4` and `Deckard Alfa (1.0.0)` has no
parseable ordering, and a parser that guessed one would fail silently. Both operands
must be versions the file declares; anything else is an error, so a mistyped
threshold stops the run instead of quietly disabling a step.

### Platform and version variables

`${platform}` is the axis that varies *inside* a single build, which is what makes it
worth branching on: one build covering Linux and Windows writes the handful of steps
that differ with an `if`, and shares the rest.

Never assemble an upstream name out of it. Writing `"url": ".../${platform}.zip"` bets
that a project names its artifacts to match, and that bet is lost the first time a
release tags `1.1-rc4` for a version called `1.1 RC4`. There are two ways to keep every
string a spec will ever fetch readable in the file. Branch with `if` and write the URLs
out:

```json
{ "step": "fetch", "if": "${platform} == Windows",
  "url": "https://example.com/releases/1.0.2/Windows.zip", "dest": ".build/package.zip" },
{ "step": "fetch", "if": "${platform} != Windows",
  "url": "https://example.com/releases/1.0.2/Linux.zip",   "dest": ".build/package.zip" }
```

Or bind the upstream spelling explicitly. `targetPlatforms` and `versions` each take a
second form: instead of an array of names, an object binding variables to each one.
This is how a build covers several platforms whose only real difference is what
upstream calls them.

```json
{
  "versions":        { "Barnard Alfa": { "tag": "v2.0.0", "infix": "Barnard-Alfa" } },
  "targetPlatforms": {
    "Linux":     { "slug": "linux",         "exe": "starship.appimage" },
    "Windows":   { "slug": "windows",       "exe": "Starship.exe" },
    "Mac-arm64": { "slug": "mac-arm64",     "exe": "Starship" },
    "Mac-x64":   { "slug": "mac-intel-x64", "exe": "Starship" }
  },
  "steps": [
    { "step": "fetch", "url": "https://example.com/download/${version.tag}/Starship-${version.infix}-${platform.slug}.zip", "dest": ".build/package.zip" },
    { "step": "defineExecutable", "executable": "install/${platform.exe}", "title": "Play" }
  ]
}
```

The keys are yours to name — a port needing a third difference adds a third key — and
the values are literal upstream strings, so nothing is guessed from the platform name.
Versions get the same treatment, which is what finally handles a version called
`1.1 RC4` released under the tag `1.1-rc4`.

Four things to know:

- **`${platform}` and `${version}` still mean the name and the version string**, not a
  bound value, so existing conditions keep working unchanged.
- **Declaration order is preserved**, because for `versions` that order is the
  hierarchy ordered conditions compare against.
- **Not every entry has to bind the same variables.** A step using one that only some
  platforms bind is legitimate — it runs for those platforms.
- **Ordered comparison works on `${version}`, not on a bound value.** `${version.tag}`
  is not a declared version, so comparing it with `>=` is an error.

A build still asserts that every version × platform combination it covers is
buildable. The table makes merging easy; it does not make a ragged matrix safe.

### Structuring a spec

Versions and platforms can be laid out three ways, and none of them is the right
answer on its own. They are not three features — they are three habits built from the
same two primitives, variables and `if`, and most specs end up mixing them.

**One build per combination.** The plainest form, and the one to reach for when the
sequences genuinely differ. A port shipping an AppImage on Linux and a zip on Windows
does not extract, does not move, and does not name its executable the same way; three
steps in common is not a shared pipeline.

```json
"builds": [
  { "targetPlatforms": ["Linux"],   "steps": [ ... ] },
  { "targetPlatforms": ["Windows"], "steps": [ ... ] }
]
```

**One build, conditional steps.** When the shape is shared and a handful of steps
differ, `if` keeps the common middle in one place and puts the differences where you
can see them side by side.

```json
{ "step": "touch", "if": "${platform} == Windows", "path": ".build/config/portable.txt" },
{ "step": "touch", "if": "${platform} != Windows", "path": ".build/portable.txt" }
```

**One build, variable tables.** When the steps are identical and only upstream's
spelling differs, the table collapses them and turns the differences into something
you can read as a table rather than diff by eye.

```json
"targetPlatforms": {
  "Mac-arm64": { "slug": "mac-arm64",     "exe": "Starship" },
  "Mac-x64":   { "slug": "mac-intel-x64", "exe": "Starship" }
}
```

Two things worth weighing when you choose.

A build spanning versions × platforms **asserts every combination of the two is
buildable**. Merging is what makes that claim, and the table makes merging easy enough
to make it carelessly. The day a version drops a platform, that build splits again —
and nothing about the JSON looks wrong when this is violated.

Conditions and tables both keep upstream's strings literal. What none of them permits
is deriving one: `"url": ".../${platform}.zip"` is the one shape to avoid, because it
bets on a naming scheme that is not yours to predict.

A reasonable default is to start with one build per platform, and merge only once you
have written the second and can see that they differ in nothing but names.

---

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
| `deletePath` | `path` | `rm -rf`, minus anything listed in `userDataPaths`. Succeeds when the path is already absent. |
| `defineExecutable` | `executable`, `title` | Record a launch target. Applies `chmod +x` on non-Windows. Moves no files. |

Multiple `defineExecutable` steps are allowed; hosts typically treat the first as the default.

### User data

A teardown sequence is usually one `deletePath install`, which is fine until the
program has written save games or a configuration file inside the tree it is about
to remove. `userDataPaths` names the paths that hold data the user owns, and every
delete leaves them alone:

```json
{
  "userDataPaths": ["install/saves", "install/game.config"],
  "uninstallSteps": [
    { "step": "deletePath", "path": "install" }
  ],
  "builds": [{
    "versions": ["1.0.0"],
    "steps": [ ... ]
  }]
}
```

**Both fields are file-level only.** A build declaring either is rejected at parse
time. Neither varies between builds in practice, and `deletePath` skips a path that is
not there — so one declaration naming every platform's and every version's leavings is
correct for all of them. A program that moved its save directory between releases lists
both places:

```json
"userDataPaths": ["install/saves", "install/data/saves"]
```

The old location is simply skipped for builds that never had it, and a player upgrading
from a release that used it keeps what they had. A real difference in *teardown* is an
`if`, which reaches `uninstallSteps` with `${platform}` and `${version}` bound to what
was actually installed.

They sit beside the builds rather than inside a step because more than one thing reads
them. A host removing an item may have no `uninstallSteps` to look at, and protecting
user data across a reinstall over an existing tree runs no uninstall sequence at all —
PortForge reads the same declaration for both.

Points worth knowing:

- Paths resolve against the **run's root directory**, not the working directory, so
  an earlier `cd` cannot change what they refer to. They must stay inside the root
  like any other path, and a bad one fails the run before the first step.
- They are literal paths, not globs, and they may nest arbitrarily deep. A delete
  that would remove a directory containing one recurses into it instead.
- A path that does not exist is not an error. A program that has never run has
  written no saves.
- Naming the path being deleted *is* an error, since honouring it would mean
  deleting nothing.
- `${name}` interpolation applies, and `forge check` reports an undeclared reference
  in a user data path just as it does in a step.

An install is protected differently from a removal, and the engine handles both.

A **removal** is covered by `deletePath` sparing the listed paths, above. An
**install** runs over whatever the last one left behind, and a build shipping its own
copy of a file the user has edited is not a delete — `deletePath` never sees it. So for
a run that is not a teardown, the engine moves the declared paths out of the tree
before the first step and moves them back after the last one, on failure as well as
success. The user's copy wins over a shipped default, because that is what preserving
it means.

Hosts say which kind of run it is with `Options.Teardown`, or by taking
`Spec.TeardownOptions` instead of `Spec.BuildOptions`. Nothing else is required: a host
that declares the paths gets both protections without knowing either exists.

The delete operation is built to survive being killed. The target is set aside with a
single rename into a `.tmp-` sibling, the declared paths are moved back into a fresh
directory, and only then is the remainder deleted — so nothing is destroyed until
everything being kept is already in its final place, and re-running after an
interruption finishes the job rather than starting over. A `.tmp-` directory beside
a path you delete this way is reserved for that purpose.

---

## What a spec can and cannot do

Forge runs specs you may not have written — a catalog of them can be synced from the internet. Four rules bound what one can do:

**Variables must be declared.** `forge check` fails on any `${name}` a step interpolates that resolves to nothing, and on any surviving unbraced `$name`.

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
platform, version := engine.HostPlatform(), file.DefaultVersion
spec := engine.Select(file.Specs, platform, version)
args, _ := spec.ResolveArgs(map[string]string{"region": "us"})
platformVars, versionVars := spec.VarsFor(platform, version)

// What `forge check` reports. A host syncing specs it did not write can run
// both before building, and fail with the name of the offending variable
// rather than six steps later with a compiler's error message.
if missing := engine.UndeclaredArgs(spec); len(missing) > 0 {
    return fmt.Errorf("spec uses variables nothing declares: %v", missing)
}
if stale := engine.UnbracedRefs(spec); len(stale) > 0 {
    return fmt.Errorf("spec writes these without braces, so they are inert: %v", stale)
}

// The spec-derived half of Options in one call: steps, dependencies, args,
// the platform's and version's bound variables, and the user data paths the
// run must protect. Assembling those by hand is one chance to forget per
// field, and forgetting the last one deletes a player's saves.
opts := spec.BuildOptions(platform, version, args, order)
opts.RootDir = installDir
opts.Providers = map[string]engine.Provider{"rom": myRomLibrary}
opts.Events = func(e engine.Event) { ui.Report(e) }
opts.Log = logFile

res, err := engine.Run(ctx, opts)
```

`Run` blocks until the sequence finishes and honours context cancellation at step boundaries, killing any running subprocess.

Removing an item takes `spec.TeardownOptions(...)` instead: the uninstall sequence,
the dependency check skipped since the build tools may be gone, and `Teardown` set so
user data is spared by `deletePath` rather than moved aside. Both constructors leave
every host-owned field alone, so anything above can be overridden after the call.

`Args` are supplied unnamespaced — `{"region": "us"}`, reached as `${args.region}` —
and the engine builds the namespaced table itself. Call `spec.ResolveArgs` first if you
want the spec's defaults applied. The names `platform` and `version` are rejected
there: nothing can collide with them now that references are namespaced, but the
reservation is kept so that stays true.

`engine.SetAside` and `engine.PutBack` are exported for hosts doing their own file
juggling, but `Run` calls them itself for any non-teardown run, so a host does not need
to.

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
