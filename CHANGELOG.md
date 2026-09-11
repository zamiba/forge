# Changelog

## Unreleased

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
