# Changelog

## v0.0.9-alpha - 2026-09-22

### Added

- **`userDataPaths` takes entries outside the tree.** An entry is still a
  string for a path inside the tree, and may now be an object —
  `{ "locationType": "linuxData", "path": "melee-pc" }` — for the user's data
  a program keeps in one of the per-user folders its platform gives it. The
  location types name those folders the way the platform's programs do
  (`linuxConfig`, `linuxData`, `windowsRoaming`, `windowsLocal`,
  `windowsDocuments`, `windowsSavedGames`, `macosApplicationSupport`), each
  for one platform, so a program that writes somewhere different on each
  declares one entry per platform. The engine parses the object form into
  `Spec.UserData`, resolves it on request (`UserDataPath.Location`, which
  says `ErrOtherPlatform` for another platform's entry) and does nothing with
  it during a run, since nothing outside the tree is a build's business;
  `Spec.UserDataPaths` and every protection built on it — `deletePath`
  sparing, the install set-aside — see only the string entries, exactly as
  before. An entry is literal, not interpolated, and is rejected without a
  type or a path, with a type the engine does not know, or with a path that
  does not stay beneath the folder. One list for both, rather than a second
  field, so an author declares the user's data in one place whichever side of
  the tree it is on.

## v0.0.8-alpha - 2026-09-16

### Added

- **Launch arguments.** `defineExecutable` takes an `args` array, carried in
  the recorded `Executable`. Install-time references — `${args.x}`,
  `${platform}`, `${version}` and the variables they bind — are interpolated
  when the step runs; a provider reference is not. It stays in the executable
  verbatim and `Executable.LaunchArgs(ctx, providers)` resolves it at launch,
  every launch, so a ROM that has moved since the install is found where it
  is now and one added afterwards is found at all. A run never asks a provider
  for a launch argument, so an install does not need the ROM to be present,
  and a path an earlier step resolved is not baked in. For the port that
  takes its disc on the command line and otherwise shows a chooser:
  `"args": ["${romPath}"]`. Additive: a spec without `args` records what it
  always did, and a host that ignores `Executable.Args` sees no change.

## v0.0.7-alpha - 2026-09-15

### Added

- **`run` can execute a file inside the run directory.** A dependency
  containing a path separator — `install/nectar-launcher` — names a file the
  steps produce rather than a command the system provides. It is skipped by the
  pre-flight `PATH` check, resolved like any other step path when invoked, and
  confined to the run directory the same way. The allowlist rule is unchanged:
  the path must be declared, and reading `dependencies` still tells you
  everything a spec can execute. A bare name is never looked up in the run
  directory, so nothing an archive contains can shadow a declared command. This
  is for the growing number of ports that ship their own installer: Open
  Nectar's extracts a game's assets from the disc and wants running at install
  time, not on first launch behind a file dialog.

- **Provider references.** Every registered provider `NAME` is readable as
  `${NAMEPath}`, and `${NAMEPath.<src>}` asks it for something by name — the
  same lookup a `copy` step's `src` makes, spaces allowed after the dot. Where
  `copy from:"rom"` brings the disc into the run, `"--rom", "${romPath}"` hands
  its path to a step that can read it where it lives, which for a 1.4 GB disc
  image that is read once and never modified is the difference between an
  install and a copy. A reference resolves the first time a step that runs
  uses it; a skipped step asks for nothing. `engine.UndeclaredArgs` leaves these
  to the host, and the new `engine.ProviderRefs` lists the providers a spec
  reads so a host can confirm it registers each one; `forge check` prints them.

- **`--provider` states a mapping, and `engine.FixedProvider` backs it.**
  `NAME=DIR` is unchanged; `NAME=FILE` answers the unnamed `${NAMEPath}`, and
  `NAME.SRC=FILE` answers `${NAMEPath.SRC}` — the key shaped like the
  reference, the value a bare path — repeatable, combining per name.
  Until now the CLI could not run a spec written against a provider with real
  content knowledge: `DirProvider` needs a src, and the empty src that
  PortForge reads as "any dump this port accepts" was an error. The CLI still
  knows nothing about ROMs; the operator states what the host would compute.

- **A glossary** at the end of the README, for the words it leans on: host,
  provider, src, run directory, local command, reference, teardown.

All of it is additive. A spec written for v0.0.6 means exactly what it meant: no
dependency in the catalog contains a separator, no step uses a `${…Path}` name,
and `${…}` with a space after the dot was left verbatim before and still is
unless a provider claims it.

## v0.0.6-alpha - 2026-09-12

### Added

- **The engine protects user data across an install itself.** `deletePath`
  sparing the declared paths only covers a removal; an install runs over the
  previous one, and a build shipping its own copy of a file the user has edited
  is not a delete. `Run` now moves the paths out of the tree before the first
  step and back after the last, on failure as well as success — so a host gets
  the protection by declaring the paths rather than by arranging it. Previously
  only PortForge did this, and any other host silently did not.

- **`Options.Teardown`** marks a run as removing an item rather than installing
  one, which is the one thing the engine cannot infer from a step list. A
  teardown is spared the set-aside, since doing both would leave every uninstall
  with an install directory holding nothing but the saves.

- **`Spec.BuildOptions` and `Spec.TeardownOptions`** fill the spec-derived half
  of `Options` — steps, dependencies, args, the platform's and version's bound
  variables, and the user data paths. Nine fields came from the spec and every
  host retyped them, which is nine chances to omit one; the cost of omitting the
  last was deleting a player's saves, as this CLI did until v0.0.5. A field added
  later now reaches every host instead of only the ones that hear about it.

## v0.0.5-alpha - 2026-09-11

### Breaking

- **Braces are now required.** `${name}` is interpolated; `$name` is not. The
  unbraced form failed in the quietest way available — `"if": "$platform ==
  Windows"` compared the literal text, was false forever, and skipped its step
  without a word — so `forge check` and `engine.UnbracedRefs` report any that
  survive. Migrating is mechanical, and the check finds every site.

- **`uninstallSteps` and `userDataPaths` are file-level only.** A build
  declaring either is now rejected at parse time. Neither varies between builds
  in practice, and `deletePath` skips a path that is not there — so one
  declaration naming every platform's and every version's leavings is correct
  for all of them, a program that moved its save directory lists both places,
  and a real difference in teardown is an `if`. Accepting them in two places
  bought nothing and left it ambiguous which one ran. A spec using the bare
  array form has no file level and must switch to the object form to declare
  either.

- **Variables are namespaced.** An argument is `${args.region}` rather than
  `${region}`. `${platform}` and `${version}` are unchanged, and those two names
  stay reserved even though the namespace makes a collision impossible — it
  keeps the option open.

### Added

- **`targetPlatforms` and `versions` take an object form** binding variables to
  each entry, for a build whose platforms differ only in what upstream calls
  them:

  ```json
  "targetPlatforms": {
    "Mac-arm64": { "slug": "mac-arm64",     "exe": "Starship" },
    "Mac-x64":   { "slug": "mac-intel-x64", "exe": "Starship" }
  }
  ```

  Keys are author-named, so a port needing a third difference adds a third key,
  and the values are literal upstream strings rather than anything derived from
  a platform name. The version form is what finally handles a version called
  `1.1 RC4` released under the tag `1.1-rc4`. Declaration order is preserved,
  since for `versions` that order is the hierarchy ordered conditions compare
  against.

- `engine.Spec.VarsFor` returns the variables a platform and version bind, for
  hosts filling in `Options.PlatformVars` and `Options.VersionVars`.

### Fixed

- The CLI never passed a spec's `userDataPaths` to the engine, so
  `forge --uninstall` deleted user data that `deletePath` was meant to keep.

## v0.0.4-alpha — 2026-09-10

- **`userDataPaths`** — the paths holding data the program's user owns, which a
  `deletePath` must leave behind. A teardown sequence is usually one
  `deletePath install`, which takes save games and configuration with it when
  those live inside the installed tree. Listing them keeps them. Declared per
  build, or once for the file as a default, like the other declarative fields.

  It sits beside `uninstallSteps` rather than inside a step because the callers
  that need it are not all steps: a host removing an item may have no
  `uninstallSteps` to read, and protecting user data across a reinstall over an
  existing tree runs no uninstall sequence at all.

  Paths resolve against the run's root rather than the working directory, may
  nest below directories the delete would otherwise remove wholesale, and are
  reported by `forge check` if they interpolate an undeclared `$name`. A
  preserved path that does not exist is not an error; one naming the path being
  deleted is.

- The delete is **crash-safe and resumable**. The target is set aside with one
  rename into a `.tmp-` sibling, preserved paths are moved into a fresh
  directory, and the remainder is deleted last — so nothing is destroyed until
  everything being kept is already in place, every move is a rename on one
  filesystem rather than a copy, and re-running after an interruption completes
  the operation instead of restarting it.

- `engine.SetAside` and `engine.PutBack` move declared paths out of a tree and
  return them, for hosts that must run an operation *over* user data rather than
  delete around it — a rebuild writing into a directory the player has saves in.
  The pair survives interruption: data already set aside is skipped rather than
  clobbered, so the next PutBack still returns it.
- `engine.Options.PreservePaths` carries the list into a run.

## v0.0.3-alpha — 2026-09-06

### Added

- `forge check` now fails on any `$name` a step interpolates that no `args`
  entry declares, `$platform` and `$version` excepted. `Interpolate` leaves an
  unknown name in the string rather than blanking it, which is right once a
  build is running and unhelpful before it starts: nothing fails until the step
  carrying the reference executes, and what surfaces then is the invoked tool's
  complaint about a nonsensical argument rather than anything naming the spec.
- `engine.UndeclaredArgs(spec)` exposes the same check to hosts, so a program
  running specs it did not write can reject one before building. It reads each
  step's original JSON, so fields belonging to a host-registered step type are
  covered as well as the builtin ones.

## v0.0.2-alpha — 2026-09-06

### Breaking

- `EvalCondition` now takes the spec file's version order and returns an error
  alongside the result.
- `LoadSpecFile` and `ParseSpecFile` return a `*SpecFile` rather than a
  `[]Spec`; the builds are in its `Specs` field.

### Added

- A spec file may be an object with file-level defaults and a `builds` array,
  as well as the bare array it was before. A build's own value replaces a
  default rather than merging with it, and only the declarative fields are
  inherited — step arrays never are, because merging two sequences has no
  obvious meaning.
- `versions` on a build, so one build can cover several versions. Declaration
  order across the file is chronological, oldest first.
- `defaultVersion` on the file, stating which version a host should offer
  first. It is separate from the newest, because the newest release is not
  always the recommended one.
- `$platform` and `$version` are injected into a copy of the arg map. Both are
  now reserved names and are rejected if a host supplies them.
- Ordered `if` comparisons: `>`, `>=`, `<`, `<=`. These resolve by position in
  the declared version order rather than by parsing the operands, because
  real-world version strings such as `1.1 RC4` do not support parsing. An
  operand that is not a declared version is an error, not a silent false.
- `forge run --version` selects the version to build. Without it the file's
  `defaultVersion` is used, else the newest declared.

### Changed

- The spec file is now named `.forge.json`. `.install.json` is still read when
  `--spec` is not given, so unconverted catalogs keep working.
- The scalar `version` field is still parsed and folded into `versions`.

## v0.0.1-alpha

First tagged release.
