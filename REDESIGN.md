# REDESIGN: a grammar-directed builder

Status: accepted, and being implemented. Phase 0 (foundations) and phase 1 (SELECT and
expressions) have landed; phases 2 to 4 are still to come. The document describes the
architecture only — no implementation details, no code to copy.

## Context

`psqlb` today takes a flat token list and renders it with one function, `walk`. Every
token carries a *kind* (`OperandKw`, `PrefixKw`, `PostfixKw`, `InfixKw`, `ListKw`,
`ClauseKw`), the kind is its Go type, and the walker keeps three pieces of state — the
separator mode, the item position, and an `inSet` flag — and decides from the kind
sequence alone where a comma goes.

The kind is an approximation of the grammar. It compresses "what may follow this token"
into two bits: does this token begin a list item, and does the token after it continue the
same item. That approximation is what the open issues are about.

- **#2 (`DISTINCT_ON`)**: `SELECT DISTINCT ON (a) a, b` needs a token that is
  parenthesised and still does not end the list item. No kind means that, so `DISTINCT_ON`
  is a Go function that carries its own parentheses. It is the only keyword that does, and
  it breaks the third golden rule.
- **#6 (`IN`, `OVER` as functions)**: the question only exists because the walker cannot
  tell what a keyword expects after it. `OVER` accepts a window name *or* a parenthesised
  window definition, and no kind can express "either of these".
- **#7 (`Op`, `Kw`, `Raw`)**: the escape hatches make the user pick a kind. The taxonomy is
  a vocabulary that SQL does not have, and picking wrong can produce SQL that runs and
  returns the wrong result: `sb.Kw("NOT IN")` inside a `SELECT` list emits
  `SELECT x NOT IN ($1, $2) y`, where Postgres reads `y` as a column alias.
- **#3 (`EXCLUDED`)** and `SET` qualifier stripping are the two standing exceptions. Both
  exist because the walker has no idea which grammar position it is in, so the information
  is smuggled in through a token type and a boolean flag.

These are one problem seen from four sides: **the renderer does not know what it is
rendering.** Adding a seventh kind, or a second parenthesis-carrying function, would push
the same problem one case further out.

## Goal

Replace the kind-directed walker with a parser that knows real PostgreSQL syntax, so that
the structure of a statement is decided by the grammar production being parsed rather than
by a per-token flag.

Acceptance criteria:

1. The six token kinds are gone. No user-visible concept remains that does not exist in
   SQL. `internal/kw` exports one keyword type instead of six.
2. Every worked example in `sql_test.go` still builds, producing the same SQL and the same
   bound arguments, after the keyword spellings are updated (see *Keyword granularity*).
3. `DISTINCT ON`, `IN`, `OVER`, `FILTER`, `EXISTS`, `ANY` and every other keyword that SQL
   follows with a parenthesised group are constants. No keyword carries its own
   parentheses. Issues #2 and #6 close.
4. A token sequence that is not valid PostgreSQL for the supported subset is reported as an
   error from `ToSQL`, naming the position. It is never rendered. In particular
   `IN, sb.Lit(1), sb.Lit(2)` becomes an error rather than the invalid string `IN $1, $2`.
5. A construct outside the supported subset is reported as an explicit "not supported yet"
   error naming the construct. Coverage grows in phases; incompleteness is a normal state
   for this package and is reported honestly rather than worked around.
6. The three golden rules still hold, and hold more strictly than they do today.

Non-goals: type safety over column types, semantic checking (does this column exist, do
these types unify), operator precedence, query optimisation, execution.

## Approach

Keep the surface syntax exactly as it is — a statement is a flat token list written in the
order SQL reads, and `sb.S` is the only parenthesis. Replace everything under it.

`ToSQL` becomes a recursive-descent parser over the PostgreSQL grammar for the supported
subset. It reads the token list left to right with a cursor, and each grammar production is
one function that consumes exactly the tokens its production covers and writes its own SQL.
Separators come from the production: the select-list production joins its items with
commas because a select list is comma-separated in SQL, not because the tokens before them
had a particular Go type. Parentheses come only from `sb.S`, and a position that requires
parentheses in SQL requires an `sb.S` and reports an error without one.

Nothing infers structure any more. The parser always knows which production it is in, so
`DISTINCT ON` needs no special kind, `OVER` accepts a name or a group because its
production says so, and `SET` knows its left-hand side is a column name because the
grammar for `SET` says so rather than because a flag was set three tokens earlier.

## The central idea

Today: **the token knows what it does.** A token's Go type decides whether a comma
precedes it, and one walker applies that rule everywhere.

After: **the position knows what the token may be.** The parser is at some point in the
grammar; that point determines which tokens are legal, how many of them this item consumes,
and what separates this item from the next.

This is the whole change. Everything below follows from it.

Three consequences worth stating explicitly:

- **Item boundaries are found, not guessed.** A list production parses one item at a time,
  and an item ends when its own grammar is complete. `ORDER BY x DESC NULLS LAST, y` needs
  no "postfix" kind: the sort-key production is `expr [ASC|DESC] [NULLS FIRST|LAST]`, so it
  consumes `DESC` and `NULLS LAST` itself and returns, and the list production then emits a
  comma before `y`.
- **The same token may mean different things in different places, and that is fine.**
  `ON` introduces a join condition inside `FROM` and names a conflict target after
  `INSERT`. `Id("users")` is a table in `FROM` and a column reference in a `WHERE`. No
  token needs a second spelling for a second role. This removes the "a keyword's kind is
  fixed, so a keyword serving two roles needs two spellings" limitation, which is what
  forces `sb.Op("NOT IN")` today.
- **An unparseable sequence is an error.** The package stops being a renderer that emits
  whatever it is given.

## Layers

```
psqlb  keyword constants, dot-imported            SELECT, FROM, WHERE, ORDER, BY, ...
  |
  v
sb     token constructors and the entry point     S, Func, Id, Lit, Raw, Op, ToSQL
  |
  v
       cursor + grammar productions               selectStmt, fromItem, expr, ...
  |
  v
       emitter: SQL text and $N binding
  |
  v
internal/kw   the single keyword type
```

Parsing and emitting are **one pass**, not two. Each production writes its fragment as it
recognises it; there is no syntax tree. The reasons are that the separator belongs in the
production that owns it, that placeholder numbering is then simply emission order, and that
a tree would be a second representation to keep in step with the grammar for no current
benefit. See *Alternatives considered* for when a tree would become worth it.

## The token model

A token is data the parser inspects, not something that renders itself. The set is closed:

| Token | Written as | What the parser sees |
|---|---|---|
| Keyword | a constant from `psqlb` | one `kw.Keyword`, compared by value |
| Identifier | `sb.Id("users.id")` | a name, possibly qualified |
| Value | `sb.Lit(v)` | a value to bind |
| Group | `sb.S(...)` | a nested token list |
| Function call | `sb.Func(name, args...)` | a name and a nested token list |
| Operator | `sb.Op("@>")` | an operator symbol |
| Hand-written expression | `sb.Raw("...", vals...)` | an opaque expression |

Two changes follow.

**One keyword type instead of six.** `internal/kw` declares `type Keyword string` and every
keyword constant. `psqlb` re-exports them (`const SELECT = kw.SELECT`), so there is a single
source of truth for the spelling and the parser compares against the same constants the user
writes. `kw` stays internal, so a user still cannot declare a keyword of their own — which
is now a stronger guarantee, because a keyword the parser does not know is a keyword the
parser cannot place.

**`Clause` becomes a sealed marker interface, renamed `Token`.** It no longer carries
`BuildSQL`; the parser switches on the concrete type. A user can no longer implement it,
which is a real loss of extensibility, paid for by the parser being able to assume it has
seen every token type. `sb.Raw` remains the way to write an expression the package does not
model.

## The parser

Hand-written recursive descent, one exported-in-spirit function per production, named after
the production in the PostgreSQL documentation it implements. The convention is that each
function carries the synopsis line from the Postgres reference page for the clause it
parses, so the grammar can be compared against the source of truth by reading.

The shape, sketched only to fix the vocabulary:

```
cursor   — the token slice and an index; peek, next, expect(keyword)
parser   — a cursor, the output builder, the bound args
```

- **Lookahead.** At most two tokens, and no backtracking. Two is needed where a split
  keyword phrase decides the branch (`ON CONFLICT` versus a join's `ON`, `IS NOT NULL`
  versus `IS NULL`, `LEFT OUTER JOIN` versus `LEFT JOIN`).
- **Groups are parsed by the caller.** `sb.S` holds tokens and has no meaning of its own.
  The production that meets it decides what it is: after `FROM` it is a subquery, after `IN`
  it is an expression list or a subquery depending on its first token, in an expression it
  is a parenthesised expression or a row constructor depending on how many items it holds,
  after `VALUES` it is one row. The parser emits `(`, runs the appropriate production over
  the inner tokens, and emits `)`. Because emission stays in order, `$N` numbering is
  unaffected by nesting, exactly as today.
- **The outermost group is the statement.** `ToSQL` parses it as a statement and emits no
  parentheses. Every nested group is parenthesised. That rule is unchanged.
- **Expressions are parsed without precedence.** The DSL requires explicit parentheses and
  the emitter never adds any, so operators are emitted in the order written and no tree is
  built. The expression production validates *shape* — an operand, then an operator, then an
  operand — and nothing more. `WHERE, a, b` becomes an error; `WHERE, a, AND, b` does not.
  This keeps the expression grammar small, and it is the reason precedence is a non-goal.

## Errors

Three classes, all returned from `ToSQL`, and nothing panics:

1. **Syntax error.** The token at index *i* is not legal at this point in the grammar. The
   message names the production and what was expected: `WHERE: expected an expression, got
   keyword FROM (token 7)`.
2. **A required token is missing.** A position that requires a group did not get one, or a
   `FROM` item that PostgreSQL requires to be aliased has no `AS`. The first is silently
   wrong today. The second is where forgetting `AS` *can* be caught, because the
   requirement comes from the Postgres grammar rather than from a guess about intent: a
   subquery in `FROM` must have an alias, so `FROM, sb.S(SELECT, ...)` with nothing after
   it is an error, and `FROM, sb.S(...), sb.Id("t")` is an error whose message names the
   fix. A `FROM` item that is not a table reference, a subquery or a function call is an
   error for the same reason.
3. **Not supported yet.** The construct is legal PostgreSQL but outside the modelled
   subset. A distinct error type so that tests can assert it and so that a user can tell
   "I wrote this wrong" from "this package has not got there yet".

The current error rules are kept: a `Raw` fragment whose `$0` marker count does not match
its value count, and a `Raw` fragment containing `$N` for N other than 0, both stay errors
for the reasons already recorded.

`nil` tokens stay supported and still mean "this token is absent"; they are dropped before
parsing. Removing a token can now make the sequence ungrammatical, and that is reported —
today it produces a string such as `WHERE a AND` that only Postgres rejects. `sb.Id("")` is
dropped as a way to spell an absent token; write `nil`.

## The golden rules in the new architecture

**1. The DSL should look like the raw SQL it produces.** Strengthened. The token kinds were
the "second vocabulary" the rule warns against, and they are gone. With word-level keywords
(below), a statement is the SQL text with the commas removed and the parentheses written as
`sb.S`.

**2. No special rule or keyword that SQL does not have.** Strengthened. `DISTINCT_ON` as a
function goes (#2). `sb.Kw` goes (#7). The `EXCLUDED` two-token rule goes (#3), and
`EXCLUDED.name` becomes an ordinary qualified name. What remains:

- Commas are not written by the user. This is #5, closed as accepted, and it is the reason
  the package exists.
- An alias must be written with `AS`, and an identifier in a list position is always the
  next item of the list. `SELECT, UsersID, sb.Id("x")` emits `SELECT users.id, x`, never
  `SELECT users.id AS x`. SQL allows the implicit form; the DSL does not. This is a
  restriction on what can be expressed rather than a construct SQL lacks, which makes it a
  smaller deviation than the ones being removed, and it is documented as a restriction.

  It cannot be checked, and no check should be attempted. `SELECT users.id x` and
  `SELECT users.id, x` differ in SQL by one comma, the DSL has no comma token, so both are
  the same token sequence and both readings are valid SQL. A heuristic — an unqualified
  name after a qualified one is probably an alias — would reject the ordinary
  `SELECT users.id, name`, which is a rule PostgreSQL does not have. The affected positions
  are the select list, `RETURNING` and `FROM`; everywhere else the position takes exactly
  one expression, so adjacency is already an error.
- `SET` stripping the table qualifier from the name that begins each assignment. Kept
  deliberately (see *Settled decisions*), and now applied at exactly one grammar position
  instead of under a flag carried through the walker.

**3. Parentheses are explicit.** Strengthened. `sb.S` becomes the only source of
parentheses without exception, and a position that requires them now demands one instead of
emitting invalid SQL. Parentheses are still never removed: `IN, sb.S(sb.S(...))` still emits
two pairs and still means a one-element list of a scalar subquery.

## What happens to each open issue

| Issue | Outcome |
|---|---|
| #2 `DISTINCT_ON` carries its own parentheses | **Closed.** Written `SELECT, DISTINCT, ON, sb.S(UsersID), UsersID, UsersName`. The select-list production knows `DISTINCT ON (...)` precedes the list, so no comma follows the group. No function, no new kind. |
| #3 `EXCLUDED` consumes the token after it | **Closed by removal.** The walker rule goes and nothing replaces it. `EXCLUDED.name` is written `sb.Id("excluded.name")`, an ordinary qualified name, so no component is added. |
| #4 `EQ`, `GTE` are not SQL spellings | **Unchanged, stays open.** Go identifiers cannot be `=` or `>=`. Unaffected by this redesign. |
| #5 commas are inserted, not written | **Unchanged, stays closed.** Now honest: commas come from the grammar rather than from a heuristic over token types. |
| #6 should `IN`, `OVER` become functions | **Closed as "no".** The objection that blocked it — `OVER` takes a name or a group — is not a problem for a parser; its production accepts either. All of them stay constants and rule 3 is preserved. |
| #7 `Op`, `Kw`, `Raw` make the user choose a kind | **Largely closed.** The kinds are gone. `sb.Kw` is removed. `NOT IN`, `IS DISTINCT FROM` and `COLLATE` are modelled and need no escape hatch. What remains is a choice between *grammar positions* — an expression (`sb.Raw`) and an operator (`sb.Op`), both of which are categories that exist in PostgreSQL. The silent-wrong-SQL case disappears: a clause fragment in an expression position is a parse error. |

Additional defects that close as a side effect: `SET (a, b) = (1, 2)` becomes supported
rather than a documented limitation; `IN, sb.Lit(1)` becomes an error; a fragment that must
attach to the token before it inside a comma-separated list no longer needs to be folded
into one `Raw`.

## Keyword granularity

Decided: **one constant per SQL keyword word**, replacing today's compound constants.

```
GROUP_BY        -> GROUP, BY
ORDER_BY        -> ORDER, BY
IS_NOT_NULL     -> IS, NOT, NULL
NULLS_LAST      -> NULLS, LAST
UNION_ALL       -> UNION, ALL
LEFT_JOIN       -> LEFT, JOIN        (and LEFT, OUTER, JOIN)
INSERT_INTO     -> INSERT, INTO
DELETE_FROM     -> DELETE, FROM
ON_CONFLICT     -> ON, CONFLICT
DO_UPDATE       -> DO, UPDATE
WITH_RECURSIVE  -> WITH, RECURSIVE
DISTINCT_ON     -> DISTINCT, ON
sb.Op("NOT IN") -> NOT, IN
sb.Op("IS DISTINCT FROM") -> IS, DISTINCT, FROM
```

Reasons: the parser handles sequences natively, so a phrase needs no constant of its own;
the number of constants drops even as coverage grows, because optional words (`OUTER`,
`ALL`, `NOT`, `RECURSIVE`, `FIRST`/`LAST`) combine instead of multiplying; and the token
list becomes the SQL text word for word, which is rule 1 at its strongest. It is also what
removes the last reason to reach for an escape hatch for a phrase the package "does not
model" — there are no phrases any more, only words.

Costs, stated plainly: `GROUP, BY` reads less well than `GROUP_BY`; and the dot-imported
package exports more short common names (`ALL`, `FIRST`, `LAST`, `BY`, `DO`, `IS`, `NULL`),
which raises the chance of a collision in a user's file. This was the one decision here that
was a matter of taste, and it stays reversible: compound constants could be reintroduced
later as values the parser expands, at the cost of two spellings for one thing.

## Scope, in phases

Nothing outside the current phase is rendered; it is reported as "not supported yet". The
package doc records the boundary.

**Phase 0 — foundations.** Token model, single keyword type, cursor, error types, emitter
and `$N` binding. No grammar yet.

**Phase 1 — SELECT and expressions.** `SELECT [ALL|DISTINCT [ON (...)]] list`, `FROM` with
a table, a subquery and an `AS` alias, `WHERE`, `ORDER BY` with `ASC`/`DESC` and
`NULLS FIRST`/`LAST`, `LIMIT`, `OFFSET`. Expressions: column references, `sb.Lit`,
`sb.Raw`, `sb.Func`, comparison and boolean operators, `sb.Op`, `NOT`, `IS [NOT] NULL`,
`IS [NOT] DISTINCT FROM`, `IN`, `BETWEEN`, `EXISTS`, `CASE`, `TYPECAST` with a type name
written as an `sb.Id`, row constructors, parenthesised expressions.

**Phase 2 — writes.** `UPDATE ... SET ... [FROM] [WHERE] [RETURNING]`, both `SET` forms;
`DELETE FROM ... [USING] [WHERE] [RETURNING]`; `INSERT INTO ... VALUES`/`... SELECT`,
`ON CONFLICT ... DO NOTHING`/`DO UPDATE SET`, `RETURNING`. This completes CRUD.

**Phase 3 — joins and grouping.** `JOIN` in all its forms with `ON` and `USING`,
`LATERAL`, `GROUP BY`, `HAVING`, `UNION`/`INTERSECT`/`EXCEPT`.

**Phase 4 — the rest of the current feature set.** `WITH [RECURSIVE]` and
`AS [NOT] MATERIALIZED`, window functions (`OVER` with a name or a definition, the `WINDOW`
clause, `FILTER`, frame clauses), `ANY`/`ALL`, `COLLATE`, `ORDER BY` inside an aggregate.

**Not planned.** DDL, `MERGE`, `TABLESAMPLE`, locking clauses, array and JSON path syntax,
transaction control. Each is a candidate for a later phase, none blocks CRUD.

Note that phases 1–3 do not cover everything `sql_test.go` demonstrates today; the window
and CTE examples land in phase 4. The old engine is not kept alive alongside the new one
(see *Migration*), so those tests are skipped between phase 1 and phase 4.

## Package and file layout

No new packages. The two-package split exists solely so that the keywords can be
dot-imported, and that reason is unchanged.

```
keywords.go             package psqlb — constants re-exported from internal/kw
sb/sb.go                package doc (the design document), public API, token constructors
sb/parse.go             cursor, statement productions
sb/expr.go              expression productions
sb/emit.go              output builder, $N binding, Raw marker substitution
sb/errors.go            error types
internal/kw/kw.go       type Keyword and every keyword constant
```

The import direction is unchanged: `psqlb` -> `sb` -> `internal/kw`, and `sb` must never
import `psqlb`.

## Public API changes

| Today | After |
|---|---|
| `sb.Clause` | `sb.Token`, sealed; no `BuildSQL` |
| `sb.Statement` | `sb.Group` (an `sb.S` may be a statement, a row or a list; only its position decides) |
| `sb.S`, `sb.Func`, `sb.Id`, `sb.Lit`, `sb.Raw`, `sb.Op` | unchanged in spelling |
| `sb.Kw` | removed |
| `sb.PrefixGroup` | removed (it existed only for `DISTINCT_ON`) |
| `DISTINCT_ON(...)` | `DISTINCT, ON, sb.S(...)` |
| `EXCLUDED, UsersName` | `sb.Id("excluded.name")` |
| compound keyword constants | word-level constants |
| `sb.Id("")` as an absent token | `nil` |

Dynamic composition is unaffected: a `[]sb.Token` is still built up and spread into
`sb.S`, and appending a clause is still appending tokens. The one new requirement is that
the result must be grammatical, which was already true of the SQL it produced.

## Testing

- `sql_test.go` keeps its role as usage documentation. Each example keeps its expected SQL
  string; only the keyword spellings change. Examples for constructs in later phases are
  skipped until their phase lands.
- `walker_test.go` is deleted. It tests a mechanism that will not exist.
- A new `grammar_test.go` replaces it, organised by production rather than by rule: one
  group of cases per clause, covering each optional element and each branch.
- A new `errors_test.go`, which has no counterpart today and is the point of the redesign:
  a table of token sequences that must be rejected, with the expected message. Every case
  from #7's measurement table belongs here, and every construct that today produces SQL
  that runs and returns the wrong result.
- Optional, once the grammar is broad: a build-tagged test that runs each generated
  statement through `EXPLAIN` on a local Postgres, to check that what the grammar accepts
  is what Postgres accepts. This is the only way to verify the premise of the redesign, and
  it needs no runtime dependency because it is a test-only, opt-in target.

## Migration

The package is experimental and has no users, so the engine is replaced in place on a
branch rather than built alongside as a second implementation. Dual maintenance would cost
more than it protects.

Order of work: phase 0, then one phase per commit, each commit updating the package doc of
`sb` in the same change — the doc comment is the design document, and a behaviour change is
incomplete without it. The `sql_test.go` examples are the acceptance criterion for each
phase; a phase is done when its examples pass unmodified except for spelling.

## Settled decisions

Decisions 1-4 were open when this document was first written; 5-7 came out of reviewing
it. All are settled.

1. **Keyword granularity: word-level.** One constant per SQL keyword word, as set out above.
   Compound constants are not kept alongside, so there is one spelling for each thing.

2. **`EXCLUDED`: no token and no constructor.** The two-token walker rule is removed and
   nothing replaces it. `EXCLUDED.name` is written `sb.Id("excluded.name")`, which is an
   ordinary qualified name and needs no component the package does not already have. The
   cost is that an existing column constant cannot be reused at that position and the name
   is repeated as a string. A constructor such as `sb.Excluded(UsersName)` can be added
   later if that proves awkward in practice; adding it later is cheap, and removing it once
   users write it would not be.

   This does not interact with decision 3: `SET` strips the qualifier from the name to the
   left of the `=`, not from the expression to its right, so `excluded.name` keeps its
   qualifier.

3. **`SET` keeps stripping the table qualifier.** `SET, UsersName, EQ, ...` emits
   `SET name = ...`, so a column constant can be reused at a position where PostgreSQL
   forbids a qualifier. This remains a rule SQL does not have and remains the one
   convenience of its kind, recorded as a `TODO:` where it is declared. It is better defined
   than it is today: the parser knows it is at the target of a `SET` assignment, so the rule
   applies at exactly one grammar position and covers the `SET (a, b) = (1, 2)` form as
   well.

4. **No escape hatch for an unmodelled clause.** An unmodelled clause is "not supported
   yet", and the answer is to model it. `sb.RawClause` is not added. The package targets
   PostgreSQL alone and will not support another dialect, so the vocabulary is finite:
   modelling all of it is a large job but a bounded one, and there is no requirement to
   finish it before the package is useful, which is what the phases are for. `sb.Raw` still
   covers an unmodelled *expression*, which is where the open-ended part of SQL actually
   sits — operators, casts, type names, vendor-specific functions. Clauses are a closed
   list; expressions are not.

5. **`::` is spelled `TYPECAST`, not `CAST`.** PostgreSQL calls `::` a typecast and
   reserves `CAST` for `CAST(x AS type)`, which is a different syntax. Keeping the current
   name would give one constant two meanings once the function form is modelled, so the
   constant is renamed before phase 1. `CAST` stays free for the function form. This does
   not fix #4 — `TYPECAST` is still not the SQL spelling — but it stops one name from
   standing for two constructs.

6. **`sb.Func` stays, unchanged.** It carries its own parentheses, but it is a constructor
   rather than a keyword, so it belongs with `sb.Id` and `sb.Lit` rather than with
   `DISTINCT_ON`: rule 3 is about keywords not carrying parentheses, and a function call in
   SQL is written with parentheses by the user in either spelling. Set aside as not
   blocking. A later option is to declare the built-in functions as Go functions, so that
   `COUNT(STAR)` and `SUM(OrdersTotal)` can be written directly; that is additive, and it
   needs an answer on how it sits with rule 3 before it is taken up.

7. **Type names are postponed.** A type name is not an identifier — `numeric(10, 2)` and
   `timestamp with time zone` do not fit `sb.Id` — but deciding how to model one is not a
   phase 1 blocker. In phase 1 the position after `TYPECAST` accepts an `sb.Id`, which
   covers `jsonb`, `int`, `text` and every other simple name, and `sb.Raw` covers the rest,
   which is where a cast already lives today. A dedicated type token can be added later
   without changing what is written for the simple case.

## Alternatives considered

**Add a seventh kind for a parenthesised group that does not end the item.** This is option
2 of #2. It fixes `DISTINCT ON` and nothing else: #6 and #7 remain, `SET` and `EXCLUDED`
remain hacks, and it adds another entry to the vocabulary that rule 1 says should not
exist. Rejected.

**Keep the walker and make the escape hatches safer** — rename `sb.Kw`, model `NOT IN` as a
constant (options 1 and 2 of #7). Cheap and compatible with this redesign, but it reduces
how often the leak is met without closing it, and it does nothing for #2 or #6. Worth doing
only if this redesign is not undertaken.

**A struct-based or fluent API** — `sb.Select{Columns: ..., From: ...}` or
`sb.Select(cols...).From(t).Where(...)`. Both are genuinely grammar-aware with no parser at
all, because the Go type system carries the structure. Both break rule 1: the result does
not read as SQL, the argument order becomes the API's rather than SQL's, and optional
clauses become optional fields. Issue #1 already rejected the keyword-as-function variant
of this for a related reason. Rejected on the golden rules.

**A distinct alias token** — `sb.Alias("x")`, accepted only after `AS`, so that an alias
written without `AS` could be rejected as such. Set aside. A user who omits `AS` writes
`sb.Id`, not `sb.Alias`, so it adds a token type without covering the case it exists for.

**Parse to a syntax tree, then print it.** A real option, and the conventional one. It
would buy multi-pass validation, statement rewriting (adding a `WHERE` condition to a built
statement), and formatting decisions that need to see the whole statement. It costs a
second representation that must track the grammar, and none of those benefits is needed
now: dynamic composition happens at the token-slice level before parsing, and multi-line
formatting can be a flag on the emitter because the emitter already owns every separator.
Deferred, not rejected — if statement rewriting is ever wanted, this is the change to make,
and the production functions are the right seam to split at.

## Risks and costs

- **Size.** The grammar for the CRUD subset is several hundred lines, against roughly 80
  for `walk`. This is the price of the parser knowing what it is parsing, and it is the
  main thing being bought and paid for here.
- **The package stops being open-ended.** Today anything can be emitted, correct or not.
  After, only what the grammar covers can be emitted at all. That is the point, but it
  means the package's usefulness is now bounded by its coverage, and coverage must keep
  growing. With decision 4 there is no clause-level escape hatch, so a clause the grammar
  does not know is a hard block until it is modelled.
- **Errors move from Postgres to build time.** Users must treat the error from `ToSQL` as
  a real outcome. It is currently near-impossible to trigger.
- **Regression risk in `$N` numbering.** Numbering is emission order, and emission order is
  token order, so it should be identical to today. The existing examples check it, and they
  are the acceptance criterion for each phase.
- **The grammar can drift from PostgreSQL.** Mitigated by naming each production after the
  Postgres reference synopsis it implements, and, optionally, by the `EXPLAIN` test.
