# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repository is

`psqlb` is a design exploration of a thin SQL builder for Postgres written in Go. It is not
an ORM and has no runtime dependencies: a statement is built into a SQL string plus a
`[]any` of arguments for the `$N` placeholders, and executing it is left to pgx or
`database/sql`.

The DSL is still being explored, so the design is expected to change. Everything lives in a
single package named `sql` (the name is chosen for dot-importing, so every exported
identifier is upper-case):

- `./sql.go` — the implementation and the design document (its package doc comment).
- `./sql_test.go` — the worked examples.
- `./walker_test.go` — the mechanical coverage of the walker rules.

An earlier design, in which keywords that join their operands with commas were Go functions
(`SELECT(a, b)`, `ORDER_BY(...)`), has been dropped. The current design writes the whole
statement as one flat token list and lets `Stm` work out where the commas go, so those
keywords are constants: `Stm(SELECT, a, b, FROM, t)`.

Nothing here has been run against a real database. The tests compare generated strings and
bound arguments only.

## Commands

```sh
go test ./...                                  # the package
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

The rule is stated in the package doc, and it is what keeps the vocabulary from becoming a
list to memorise. Read the doc comment before adding a keyword: a keyword is a function
exactly when SQL always follows it with one parenthesised group (`IN`, `EXISTS`, `ANY`,
`OVER`, `FILTER`, `DISTINCT_ON`), or when it takes a bare name (`INSERT_INTO`,
`ON_CONFLICT`). Every other keyword is a constant.

## Conventions

The package doc comment is the design document — it is long by intent and records the
rationale, the deliberate omissions (no type safety, no arity checking, no sugar) and the
known limitations. A change to behaviour is incomplete until the relevant section of the
doc comment is updated with it.

`sql_test.go` is usage documentation as well as a test: each example states the SQL it must
produce with the expected string broken into lines matching the Go lines above it.
`walker_test.go` is the separate mechanical coverage of the walker rules. Add examples to
the former, rule coverage to the latter.

Commit messages are lower-case imperative subjects with long prose bodies that explain why
the design changed, what it costs, and what the tests prove about it. Match that.
