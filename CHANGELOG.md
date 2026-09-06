# Changelog

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
