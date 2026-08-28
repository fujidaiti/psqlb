# Function keywords: where issues #1 and #6 stand after the parser rewrite

This note consolidates a discussion of issue #6 ("Should IN, OVER and similar
parenthesised keywords become functions?") in the light of the current
implementation, and of issue #1 ("Consider keywords usable with or without
parentheses"), which turns out to be the stronger form of the same proposal.

Nothing here changes the code. The conclusion is that the proposal should stay
rejected, but for a different reason than the one recorded in either issue.

## Context

Both issues were written against an earlier design and rest on premises that no
longer hold.

- The walker that inferred structure from token kinds is gone. `ToSQL` is a
  hand-written recursive-descent parser, and every separator belongs to the
  production that owns it rather than to the keyword.
- `Stm` and `Row` were merged into one group constructor, now `sb.P`. There is
  one separator rule, decided by the position, not by the constructor.
- `DISTINCT_ON` is no longer a function. It is written `DISTINCT, ON, sb.P(...)`
  (`sb/sql_test.go:209`), so no keyword carries its own parentheses any more.
  `sb.F` is the only constructor that does.
- `NOT IN` is a native keyword sequence, parsed at `sb/expr.go:90`. It is no
  longer written with `sb.Op("NOT IN")`.
- The keywords moved into package `kw`, which declares constants only and must
  never import `sb`.

## The proposal in its two forms

**Form A (issue #6): replace the constant with a function.**

```go
UsersID, IN(sb.V(1), sb.V(2))   // instead of UsersID, IN, sb.P(sb.V(1), sb.V(2))
```

**Form B (issue #6 combined with #1): the keyword is both a bare token and
callable.**

```go
UsersID, IN, sb.P(sb.V(1), sb.V(2))   // bare form still legal
UsersID, IN(sb.V(1), sb.V(2))         // call form also legal

sb.F("SUM", y), OVER, sb.I("w")           // bare form, named window
sb.F("SUM", y), OVER(PARTITION, BY, x)    // call form, window definition
```

Form B is the one worth arguing about. Form A is strictly worse and is disposed
of first.

## Why Form A fails

1. **`OVER` cannot be a function.** Its right-hand side is either a
   parenthesised window definition or a bare window name, and `over()` accepts
   both today (`sb/window.go:33-46`). A function form cannot express the second.
   `OVER` would have to remain a constant, or exist as both, which is Form B.

2. **`IN` loses its symmetry with `NOT IN`.** Since `NOT IN` became a native
   keyword sequence, `IN, sb.P(...)` and `NOT, IN, sb.P(...)` have the same
   shape. A function form breaks a symmetry that the parser rewrite created, and
   `NOT IN` has no function form to match, because it is two keywords in SQL.

3. **Golden rule 3 currently has no exception.** The rule is that parentheses
   wanted in the SQL string must be written in the DSL with `P`, the only group
   there is. Removing `DISTINCT_ON` made that literally true. Form A
   reintroduces the exception that the rewrite removed, and this time there is no
   walker limitation forcing it: `IN` and `OVER` work as constants.

Note that the mechanical objection recorded in the original text of #6 — that a
function has to fix the separator inside its parentheses and these keywords do
not agree on one — is dead. `Group` already carries a `name` emitted before the
opening parenthesis (`sb/sb.go:397`), so `IN(x, y)` is representable as
`Group{name: "IN"}` with no new machinery. Feasibility was never the problem.

## Form B fixes the first two objections

Objections 1 and 2 are both about the function form *replacing* the constant.
The dual form replaces nothing: `OVER` keeps its bare spelling for the named
window, and `NOT, IN, sb.P(...)` keeps its symmetry with `IN, sb.P(...)`.

It is also worth recording that **issue #1 was closed for a reason that no
longer exists.** Its Variant A was rejected because a bare `SELECT, a, b` would
be joined with spaces while `SELECT(a, b)` joined with commas, producing invalid
SQL silently. That failure depended on the keyword carrying its own separator.
Under the recursive-descent parser the separator belongs to the production, so
both spellings emit the same string and the failure class is gone. The Variant B
enumeration in #1 is likewise no longer the relevant test: it asked which
keywords are parenthesised depending on operand count, whereas #6 targets exactly
the keywords that are *always* parenthesised, which Variant B set aside.

## Why Form B still fails

A dual-form keyword cannot be a constant. It has to be a package-level variable
of a function type:

```go
type kwFunc func(...Token) Token

var IN kwFunc = func(items ...Token) Token { /* ... */ }
```

Three consequences, all in `kw` and `sb`, which is the centre of the design
rather than its edge.

1. **Keywords stop being comparable, and the keyword set becomes
   heterogeneous.** `tok.Keyword` is a string type, and the parser matches with
   `t == kw.IN` inside a type switch (`sb/expr.go:60-79`). Go function values are
   not comparable, so every production that can see a converted keyword needs a
   second type case, and some keywords are string constants while others are
   function values.

2. **Keywords stop being immutable.** `kw.IN = nil` would compile. The package
   doc's claim that a user cannot declare or alter a keyword — the reason the
   token types live in `internal/tok` — stops being true.

3. **The import direction breaks.** `IN(x, y)` must return the parenthesised
   group type, which lives in `sb`, and `kw` must never import `sb`. `Group`
   would have to move to `internal/tok`, with `sb.P` and `sb.F` becoming
   re-exports.

Golden rule 3 is untouched by the combination, and arguably worse off: there
would be two spellings of the same SQL, so a reader has to recognise both and
every example and test has to choose one.

## Conclusion

Keep both issues closed. The cost has moved from "the function form cannot
express `OVER`" — which Form B answers — to "keywords stop being constants",
which it does not.

Suggested follow-up, if the issues are to be kept accurate:

- Reopen or annotate **#1** to record that its Variant A argument was invalidated
  by the parser rewrite, and that its Variant B enumeration no longer covers the
  case #6 asks about.
- Update **#6** to state that the dual form is the strongest version of the
  proposal, and that the objection to it is the constant-versus-variable one
  above, so the next reader starts from the real cost.
- Issue **#7**'s framing is stale wherever it says `sb.Op("NOT IN")` is the
  required spelling.

---

## Review: the conclusion is right, the stated reason is not

Appended after reviewing the note against the current code. Every factual claim
above checks out: `DISTINCT, ON, sb.P(...)` at `sb/sql_test.go:209`, `NOT IN`
parsed natively at `sb/expr.go:78-90`, `over()` accepting both a window name and
a group at `sb/window.go:33-46`, and `Group.name` already emitted before the
opening parenthesis at `sb/sb.go:397-402`. The issue texts themselves were not
available, so the characterisation of #1's Variant A and of #7's framing is taken
on this note's word.

The conclusion — keep both issues closed — is correct. The reason given for it is
not, and that matters here more than it usually would, because this note's own
thesis is that #1 was closed for a reason that later stopped being true.

### Form B does not require a variable

The section "Why Form B still fails" opens with "A dual-form keyword cannot be a
constant." That is false. There is a third implementation the note does not
consider: a method on the keyword type.

```go
// in internal/tok, alongside the type
func (k Keyword) Of(items ...Token) Group {
	return Group{name: string(k), items: items}
}
```

```go
UsersID, IN, sb.P(sb.V(1), sb.V(2))       // bare form
UsersID, IN.Of(sb.V(1), sb.V(2))          // call form
sb.F("SUM", y), OVER, sb.I("w")           // bare form, named window
sb.F("SUM", y), OVER.Of(PARTITION, BY, x) // call form, window definition
```

Go permits a value-receiver method call on a typed constant, so `kw.IN` can stay
`tok.Keyword = "IN"` and still be callable. This was compiled and run to confirm
it rather than reasoned about. All three consequences listed above are therefore
avoided:

1. **Comparability is preserved.** `kw.IN` remains a string constant, so
   `switch t { case kw.IN: }` at `sb/expr.go:60-79` still compiles and still
   matches. The keyword set stays homogeneous, and no production needs a second
   type case.

2. **Immutability is preserved.** `kw.IN = nil` still does not compile. The claim
   at `sb/sb.go:50-52` that a user cannot declare or alter a keyword remains
   true.

3. **The import direction is preserved.** `kw` imports nothing new; the method is
   declared in `internal/tok` where the type is. The only structural cost is that
   `Group` moves to `internal/tok`, with `sb.Group` becoming a type alias and
   `P` and `F` staying where they are as its constructors. `sb.Token` is already
   an alias of `tok.Token` (`sb/sb.go:47-49`), so that mechanism is part of the
   design already rather than something introduced for this.

Consequently, "keywords stop being constants" is not a cost of the proposal. It
is a cost of one way of implementing the proposal. A rejection resting on it
stops holding as soon as the next reader notices the method form — which is the
same pattern the note identifies in #1, where the Variant A objection stopped
holding once the separator moved from the keyword to the production.

### The argument that does hold is the last paragraph, not the numbered list

The paragraph at the end of "Why Form B still fails" — two spellings of the same
SQL, so a reader has to recognise both and every example and test has to choose
one — is the only argument in the section that does not depend on how the feature
is implemented. It follows from golden rules 1 and 3 directly, and no amount of
Go typing removes it:

- Every reader of a statement has to know both forms, because both appear in
  real code.
- `sql_test.go` is usage documentation. It must pick one spelling per example and
  therefore stops documenting the other.
- `errors_test.go` grows a second row for every rejected sequence that involves a
  parenthesised keyword, because each has two spellings to reject.

Golden rule 2 adds a second implementation-independent objection, which the note
does not make. `IN.Of(x, y)` does not look like the SQL it produces: `Of` is a
word SQL does not have, and it is exactly the "second vocabulary" that golden
rule 1 exists to prevent. Any dual form needs some such spelling, because the
bare constant has already taken the only name SQL offers.

Together those two are a rejection on the rules alone, with no reference to Go's
type system. That rejection stays valid through the next rewrite, which is what
this note is trying to achieve.

### Form A objection 1 is overstated

"`OVER` cannot be a function" is too strong, and invites the same "here is how"
reply that Form B is. A function form can express `OVER window_name`:
`OVER(sb.I("w"))` would inspect its argument and omit the parentheses when it is
a lone `I`. The real objection is that doing so adds a rule SQL does not have and
makes a group's parentheses conditional on its contents — golden rules 2 and 3,
the same pair that settles Form B. Same verdict, but stated as a rule violation
rather than as an impossibility it cannot be answered with a workaround.

### Revised follow-up

The three suggested annotations stand, with one change to the second:

- **#6** should record that the dual form is the strongest version of the
  proposal, and that the objection to it is that SQL has one spelling per
  construct and the DSL would have two, plus the fact that any call form needs a
  method name SQL does not contain. It should not record the
  constant-versus-variable argument as the reason, since a method on
  `tok.Keyword` satisfies it; if that argument is mentioned at all, it should be
  as a note that the mechanics are solvable and are not what decides the
  question.
