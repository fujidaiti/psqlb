# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repository is

`psqlb` is a design exploration of a thin SQL builder for Postgres written in Go. It is not
an ORM and has no runtime dependencies: a statement is built into a SQL string plus a
`[]any` of arguments for the `$N` placeholders, and executing it is left to pgx or
`database/sql`.

The repository holds **two competing designs of the same DSL**, both in a package named
`sql` (the name is chosen for dot-importing, so every exported identifier is upper-case):

- `./idea.go` + `./idea_test.go` — the original design. Keywords that join their operands
  with commas are Go functions: `SELECT(a, b)`, `ORDER_BY(...)`.
- `./flat/sql.go` + `./flat/sql_test.go` + `./flat/walker_test.go` — the newer design. The
  whole statement is one flat token list and `Stm` works out where the commas go, so those
  keywords become constants: `Stm(SELECT, a, b, FROM, t)`.

`flat` is a port of the root package, not a refactor of it: the root package is left
untouched, and every example in `idea_test.go` was ported to `flat/sql_test.go`, so the
two designs can be compared example by example. When changing one design, do not assume the other should follow.

Nothing here has been run against a real database. The tests compare generated strings and
bound arguments only.

## Commands

```sh
go test ./...                                  # both packages
go test ./flat/                                # one package
go test ./flat/ -run TestCommaPlacement        # one test
go test ./flat/ -run 'TestKindsAtEveryPosition/head/prefix'  # one subtest
go vet ./... && gofmt -l .                     # both must stay clean
```

Go 1.27 (see `go.mod`).

## Architecture

### The shared core (both designs)

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

### `./idea.go` — parenthesisation by position

`Stm` joins with spaces; keyword functions join with commas. A nested `Statement` is
parenthesised **only when the enclosing separator is a space**, since SQL never
parenthesises a single item of a comma-separated clause. That one rule produces both
`ORDER_BY(Stm(UsersID, DESC))` (no parens) and `WHERE, Stm(a, OR, b)` (parens).

`Raw` **panics** on a malformed fragment; `ToSQL` returns no error.

### `./flat/sql.go` — parenthesisation by nesting, commas by token kind

`Stm` takes the whole statement as one flat token list and a walker decides the separators.
Every token carries a `kind`, and the kind is its Go type (`operandKw`, `prefixKw`,
`postfixKw`, `infixKw`, `listKw`, `clauseKw`). The four expression kinds are the product of
two independent questions — does this token begin a new list item, and does the token after
it continue the same item. The other two set the separator: a list keyword opens a
comma-separated list, a clause keyword closes it and returns to spaces. **Only operands and
prefixes ever take a comma, and only when a list is open and the previous item has ended.**

Three invariants in `walk` are load-bearing and easy to break:

1. Each token is rendered *before* its separator is chosen, and an empty render is skipped
   without advancing `pos` or `m`. That is what lets a `nil` token stay in place as an
   optional item without leaving a dangling comma.
2. Kinds are read from the Go type before building, which is how `SET` can strip a table
   qualifier from the name that begins each assignment without threading a mode through
   `BuildSQL`. `Id` and `excludedKw` are intercepted by the walker, so their own
   `BuildSQL` methods are never reached from `walk`.
3. A nested `Stm` is *always* parenthesised and `Row` always is; they run the same walker
   and differ only in the starting mode. There is no way to nest without parentheses — to
   reuse a fragment where parens are unwanted, keep it as a `[]Clause` and spread it.
   `IN(inner...)` is a membership test; `IN(Stm(inner...))` is a scalar comparison.

`ToSQL` returns an error and nothing panics. Errors are reported only where the alternative
is emitting a string Postgres rejects (Raw marker/value count mismatch, `EXCLUDED` not
followed by an `Id`), plus one case where the string *would* run and that is exactly the
problem: a `Raw` fragment containing `$N` for N other than 0 silently reads another
clause's value.

### When a keyword is a constant and when it is a function

Each design states its own rule in its package doc, and the rule is what keeps the
vocabulary from becoming a list to memorise. Read the doc comment before adding a keyword.

- `idea.go`: a function only when it shapes its operands — joins them with commas, or takes
  a bare `Id` rather than a `Clause`.
- `flat`: a function exactly when SQL always follows it with one parenthesised group
  (`IN`, `EXISTS`, `ANY`, `OVER`, `FILTER`, `DISTINCT_ON`), or when it takes a bare name
  (`INSERT_INTO`, `ON_CONFLICT`).

## Conventions

The package doc comments are the design document — they are long by intent and record the
rationale, the deliberate omissions (no type safety, no arity checking, no sugar) and the
known limitations. A change to behaviour is incomplete until the relevant section of the
doc comment is updated with it.

`idea_test.go` and `flat/sql_test.go` are usage documentation as well as tests: each example
states the SQL it must produce with the expected string broken into lines matching the Go
lines above it. `flat/walker_test.go` is the separate mechanical coverage of the walker
rules. Add examples to the former, rule coverage to the latter.

Commit messages are lower-case imperative subjects with long prose bodies that explain why
the design changed, what it costs, and what the tests prove about it. Match that.
