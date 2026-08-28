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
    . "github.com/fujidaiti/psqlb/kw"
    "github.com/fujidaiti/psqlb/sb"
)

sb.ToSQL(SELECT, UsersID, FROM, Users, WHERE, UsersAge, ">=", 18)
```

The module root holds no Go files.

- `./kw/kw.go` — `package kw`: every SQL keyword constant and nothing else, not even the
  type they are declared with. This is the package that is dot-imported. There are no
  operators here: an operator is written as its SQL symbol.
- `./sb/sb.go` — `package sb`: the design document (its package doc comment), the token
  constructors and the entry point. `Token`, `I`, `Arg`, `RawExpr`, `Group`, `P`, `F`,
  `ToSQL`.
- `./sb/parse.go` — the cursor, the statement dispatcher, `WITH`, and the SELECT
  productions.
- `./sb/write.go` — the INSERT, UPDATE and DELETE productions.
- `./sb/window.go` — `FILTER`, `OVER`, the `WINDOW` clause and frame clauses.
- `./sb/expr.go` — the expression productions.
- `./sb/norm.go` — normalization: the boundary that turns a plain Go value into a token,
  and PostgreSQL's lexical rule for an operator name.
- `./sb/emit.go` — the output builder, `$N` binding and `RawExpr` marker substitution.
- `./sb/errors.go` — the three error types.
- `./internal/tok/tok.go` — `package tok`: `Token`, `Keyword` and `Operator`, and nothing
  else. The types are here rather than in `kw` because `kw` is dot-imported and must bring
  SQL words into scope alone. `sb` aliases `Token` out of it, since a user can see that one
  as the return type of `Arg` and `RawExpr`; the rest stays internal, so a user cannot
  declare a keyword.
- `./sb/sql_test.go`, `./sb/grammar_test.go`, `./sb/errors_test.go`,
  `./sb/example_test.go` — `package sb_test`, all four external, so every test is written
  exactly the way a user writes a statement: the keywords by dot-import, everything else
  through `sb`.

The import direction is `sb` → `kw` → `internal/tok`, and `sb` imports `internal/tok`
directly as well. `kw` must never import `sb`.

Nothing here has been run against a real database. The tests compare generated strings and
bound arguments only.

## Commands

```sh
go test ./...                                  # all three packages
go test ./... -run TestExpressions              # one test
go test ./... -run 'TestExpressions/IS_NULL'    # one subtest
go vet ./... && gofmt -l .                     # both must stay clean
```

Go 1.27 (see `go.mod`).

## Architecture

### The central idea

**The position knows what the token may be.** `ToSQL` is a hand-written recursive-descent
parser over the PostgreSQL grammar. At every point it knows which production it is in, and
that decides which tokens are legal there, how many of them the current item consumes, and
what separates this item from the next.

An earlier design gave every token a *kind* (its Go type) and let one walker decide from
the kind sequence alone where a comma went. That kind was an approximation of the grammar,
and it is gone. Nothing infers structure any more.

Three consequences, all load-bearing:

1. **Item boundaries are found, not guessed.** A list production parses one item at a time
   and the item ends when its own grammar is complete. The sort key is
   `expression [ASC|DESC] [NULLS FIRST|LAST]`, so it consumes its own modifiers and the
   list production writes the comma before the next key.
2. **The same token means different things in different places.** A group is a subquery
   after `FROM`, an expression list after `IN`, a row constructor in an expression. No
   token needs a second spelling for a second role.
3. **An unparseable sequence is an error.** The package is no longer a renderer that emits
   whatever it is given.

### Parsing and emitting are one pass

Each production writes its own SQL fragment as it recognises it. There is no syntax tree.
The separator therefore belongs to the production that owns it, and `$N` numbering is
simply emission order, which is token order — so a nested statement numbers correctly at
any depth with no counter and no renumbering pass.

`emitter` owns every separator: `word` (separated), `glue` (not separated), `comma`,
`open`/`close` (the only place a parenthesis is written) and `bind`. A production that
wants a comma asks for one.

At most two tokens of lookahead, no backtracking, and no operator precedence. The
expression grammar validates *shape* — an operand, then an operator, then an operand — and
nothing more, which is what keeps it small.

### Tokens

`sb.Token` is `tok.Token`, a marker interface with one exported method. The concrete types
are `tok.Keyword`, `tok.Operator`, `sb.I`, `value`, `sb.RawExpr`'s `rawFrag`, `unspread`
and `sb.Group` (which `sb.P` and `sb.F` both build). The parser switches on them; anything
else is an error.

`ToSQL`, `P` and `F` take `...any`, and `normalize` in `sb/norm.go` turns each item into
one of those types before the parser sees anything: a token stays what it is, a `string`
that satisfies PostgreSQL's lexical rule for an operator name becomes `tok.Operator`,
`"::"` becomes the typecast keyword, a `[]any` or `[]Token` becomes `unspread`, and
everything else — `nil` included — becomes a `value` to bind. `sb.Arg` is the override, for
a value whose Go string would otherwise lex as an operator, such as a `%` LIKE pattern.

`Token` gates nothing at compile time any more. `sb.ToSQL(SELECT, "id")` compiles and
produces `SELECT $1`, which is legal SQL that returns the wrong rows; that is the price of
writing operators and values as themselves, and it is recorded as a limitation in the
package doc rather than worked around. A reusable fragment is a `[]any`, since Go will not
spread a `[]Token` into a variadic `any`.

An operator in a position that wants an operand is always an error, which is what catches a
misplaced one such as `sb.ToSQL(SELECT, "=")`. `"*"` is a well-formed operator name, so it
is multiplication in an operator position and the whole row only where PostgreSQL allows
the whole row: a select-list or `RETURNING` item, and the sole argument of a call.

Every keyword is **one word**. Phrases are written as the words they are made of: `GROUP,
BY`, `IS, NOT, NULL`, `LEFT, OUTER, JOIN`. The parser reads sequences natively, so a phrase
needs no constant of its own and optional words combine instead of multiplying. The keyword
constants are declared once, in `kw`, and the parser compares against those same constants,
so the spelling a user writes and the value the parser compares against are the same thing
and a misspelling in a production does not compile.

`sb.RawExpr` is **one opaque expression**. The parser checks where it may appear and never
looks inside the string.

### Errors

Three types in `sb/errors.go`, all returned from `ToSQL`, and nothing panics:
`SyntaxError` (the token is not legal here, with a `Fix` naming the way to write it where
the mistake has a known shape — an operator where an operand belongs names `sb.Arg`),
`MissingError` (the position requires
something absent — a group where SQL parenthesises, the alias PostgreSQL requires on a
subquery in `FROM`), and `UnsupportedError` (legal PostgreSQL, outside the modelled
subset). Coverage grows in phases; incompleteness is reported honestly rather than worked
around.

Two `RawExpr` errors are kept from the previous design: a `$0` marker count that does not
match the value count, and a fragment containing `$N` for N other than 0.

`nil` is bound as a parameter. It used to mean "this token is absent" and was dropped
before parsing; it no longer is, because a bare Go value cannot be told from an absent
token, so dropping it would let the data in a statement change its shape. That also keeps
the parser's own `nil` sentinel honest: `normalize` wraps a `nil` item into a `value`, so a
`nil` reaching `lookahead` means the end of the input and nothing else.

A slice passed without `...` is reported too: `ToSQL(parts)` compiles and would otherwise
bind the whole statement as one parameter.

### The golden rules

Three rules govern the design, all stated at the top of the package doc of `sb`: the DSL
should look like the raw SQL it produces, no special rule or keyword may be introduced that
SQL does not have, and parentheses are explicit — if the SQL string needs them, the DSL
must spell them with `P`, the one group there is. `P` is always parenthesised; the
statement is written with `sb.ToSQL(...)`, which is the one form that is not. Read that
section before changing anything.

Every keyword is a constant; no keyword carries its own parentheses. `sb.F` is the one
constructor that does, and it is a constructor rather than a keyword, so it sits with
`sb.I` and `sb.Arg`.

Two restrictions remain and are documented as such: an alias must be written with `AS`
(which cannot be checked, and no check should be attempted — see the doc comment), and
`SET` strips the table qualifier from the name that begins each assignment. The second is
the only rule the package has that SQL does not; it is recorded as a `TODO:` on `setList`
in `sb/write.go`. Add to it rather than resolving it silently.

### Scope

All four phases are in: `SELECT` with every clause, joins in all their forms, `LATERAL`,
`GROUP BY`, `HAVING`, set operations, `WITH [RECURSIVE]`, window functions with frames, the
three write statements with `RETURNING` and `ON CONFLICT`, and the expression grammar.
Every example in `sql_test.go` passes and none is skipped.

The `# Scope` section of the `sb` package doc lists what is still not modelled — type names
with modifiers, `CAST(x AS type)`, frame exclusion, `ORDER BY ... USING`, `ON CONFLICT ON
CONSTRAINT`, locking clauses, DDL, `MERGE`. There is no escape hatch for an unmodelled
clause by design: the answer is to add the production. `sb.RawExpr` covers an unmodelled
expression.

## Conventions

The package doc comment of `sb` is the design document — it is long by intent and records
the rationale, the deliberate omissions and the known limitations. A change to behaviour is
incomplete until the relevant section of the doc comment is updated with it.

Each production function carries the synopsis line from the PostgreSQL reference page it
implements, so the grammar can be compared against the source of truth by reading. Keep
that up when adding a production.

`sql_test.go` is usage documentation as well as a test: each example states the SQL it must
produce with the expected string broken into lines matching the Go lines above it.
`grammar_test.go` is the mechanical coverage, organised by production. `errors_test.go` is
the table of sequences that must be rejected. Add examples to the first, production
coverage to the second, and every newly rejected sequence to the third.

Commit messages are lower-case imperative subjects with long prose bodies that explain why
the design changed, what it costs, and what the tests prove about it. Match that.
