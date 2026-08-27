# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repository is

`psqlb` is a design exploration of a thin SQL builder for Postgres written in Go. It is not
an ORM and has no runtime dependencies: a statement is built into a SQL string plus a
`[]any` of arguments for the `$N` placeholders, and executing it is left to pgx or
`database/sql`.

The DSL is still being explored, so the design is expected to change. It is split across
three packages, so that a user can dot-import the SQL keywords alone and reach everything
else through the `sb.` prefix:

```go
import (
    . "github.com/fujidaiti/psqlb"
    "github.com/fujidaiti/psqlb/sb"
)

sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersAge, GTE, sb.Lit(18))
```

- `./keywords.go` — `package psqlb`: the SQL keyword constants and `DISTINCT_ON`, nothing
  else. This is the package that is dot-imported.
- `./sb/sb.go` — `package sb`: the implementation and the design document (its package doc
  comment). `Clause`, the walker, `S`, `Func`, `Id`, `Lit`, `Raw`, `Op`, `Kw`.
- `./internal/kw/kw.go` — `package kw`: the token kinds and the six keyword types, shared
  by the two packages above. Internal, so a user cannot name a keyword type and therefore
  cannot declare a keyword; `sb.Op` and `sb.Kw` are the way to write one.
- `./sql_test.go`, `./walker_test.go` — `package psqlb`, so the keywords are in scope bare,
  exactly as a dot-import gives them. The worked examples and the walker coverage.
- `./example_test.go` — `package psqlb_test`: one example that checks the intended import
  style compiles from outside.

The import direction is `psqlb` → `sb` → `internal/kw`. `sb` must never import `psqlb`.

An earlier design, in which keywords that join their operands with commas were Go functions
(`SELECT(a, b)`, `ORDER_BY(...)`), has been dropped. The current design writes the whole
statement as one flat token list and lets `S` work out where the commas go, so those
keywords are constants: `sb.S(SELECT, a, b, FROM, t)`.

Nothing here has been run against a real database. The tests compare generated strings and
bound arguments only.

## Commands

```sh
go test ./...                                  # all three packages
go test ./... -run TestCommaPlacement          # one test
go test ./... -run 'TestKindsAtEveryPosition/head/prefix'  # one subtest
go vet ./... && gofmt -l .                     # both must stay clean
```

Go 1.27 (see `go.mod`).

## Architecture

### The core

`Clause` is the single interface. It takes the args bound so far and returns its SQL
fragment plus those args with whatever it bound appended. Passing args along this chain is
the entire `$N` numbering scheme — there is no global counter and no renumbering pass, so
a nested statement numbers correctly at any depth and statements are values that can be
composed later.

Every position in the DSL takes a `Clause`, never `any`. A plain Go value is not a token;
it enters only through `Lit(v)` (or the `$0` markers of `Raw`). This is deliberate:
`SELECT("id")` would otherwise compile and produce a query Postgres accepts and runs while
returning the wrong rows.

The keyword constants work because their underlying types are `string`, which is what lets
them be declared with `const`.

### Parenthesisation by nesting, commas by token kind

`S` takes the whole statement as one flat token list and a walker decides the separators.
Every token carries a kind, and the kind is its Go type (`kw.OperandKw`, `kw.PrefixKw`,
`kw.PostfixKw`, `kw.InfixKw`, `kw.ListKw`, `kw.ClauseKw`, all in `internal/kw`). The four expression kinds are the product of
two independent questions — does this token begin a new list item, and does the token after
it continue the same item. The other two set the separator: a list keyword opens a
comma-separated list, a clause keyword closes it and returns to spaces. **Only operands and
prefixes ever take a comma, and only when a list is open and the previous item has ended.**

Three invariants in `walk` are load-bearing and easy to break:

1. Each token is rendered *before* its separator is chosen, and an empty render is skipped
   without advancing `pos` or `m`. That is what lets a `nil` token stay in place as an
   optional item without leaving a dangling comma. A nested `S` never renders empty, since
   it always emits its parentheses, so an empty group is *not* an optional token.
2. Kinds are read from the Go type before building, which is how `SET` can strip a table
   qualifier from the name that begins each assignment without threading a mode through
   `BuildSQL`. `Id` and `kw.ExcludedKw` are intercepted by the walker, so their own
   `BuildSQL` methods are never reached from `walk`.
3. A nested `S` is *always* parenthesised, empty or not, and the outermost level never is.
   Every group starts comma-separated and a clause keyword switches it to spaces, which is
   the same rule that governs the middle of a token list. There is no way to nest without
   parentheses — to reuse a fragment where parens are unwanted, keep it as a `[]Clause` and
   spread it. `IN, S(inner...)` is a membership test; `IN, S(S(inner...))` is a scalar
   comparison.

`ToSQL` returns an error and nothing panics. Errors are reported only where the alternative
is emitting a string Postgres rejects (Raw marker/value count mismatch, `EXCLUDED` not
followed by an `Id`), plus one case where the string *would* run and that is exactly the
problem: a `Raw` fragment containing `$N` for N other than 0 silently reads another
clause's value.

### The golden rules

Three rules govern the design, all stated at the top of the package doc of `sb`: the DSL
should look like the raw SQL it produces, no special rule or keyword may be introduced that
SQL does not have, and parentheses are explicit — if the SQL string needs them, the DSL
must spell them with `S`, the one group there is. Read that section before changing
anything.

Every keyword is therefore a constant. `DISTINCT_ON` is the sole function, because as a
prefix it must stay glued past its own parentheses to the first item of the SELECT list.
`SET` stripping the table qualifier and `EXCLUDED` consuming the `Id` after it are the two
standing exceptions, kept for usability. They are recorded as `TODO:` comments where they
are declared; add to those rather than resolving one silently.

## Conventions

The package doc comment of `sb` is the design document — it is long by intent and records
the rationale, the deliberate omissions (no type safety, no arity checking, no sugar) and the
known limitations. A change to behaviour is incomplete until the relevant section of the
doc comment is updated with it.

`sql_test.go` is usage documentation as well as a test: each example states the SQL it must
produce with the expected string broken into lines matching the Go lines above it.
`walker_test.go` is the separate mechanical coverage of the walker rules. Add examples to
the former, rule coverage to the latter.

Commit messages are lower-case imperative subjects with long prose bodies that explain why
the design changed, what it costs, and what the tests prove about it. Match that.
