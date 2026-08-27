# TODO

Open design questions. Each one is a known deviation from the golden rules in the
package doc comment of `sql.go`, deferred rather than accepted.

## 1. `DISTINCT_ON` still carries its own parentheses

`DISTINCT_ON` is the only keyword left as a function. Every other keyword that SQL
follows with a parenthesised group — `IN`, `ANY`, `FILTER`, `OVER`, `EXISTS`,
`INSERT_INTO`, `ON_CONFLICT` — is now a constant, and the group after it is written
with `Row` or `Stm`.

`DISTINCT_ON` cannot follow them, because it is the only prefix among them. Written
as a constant it would set glue, so the group after it takes no comma, but the group
is an operand and therefore ends the item, so the first column of the SELECT list
takes one:

```go
SELECT, DISTINCT_ON, Row(UsersID), UsersID, UsersName
// SELECT DISTINCT ON (users.id), users.id, users.name   -- wrong
```

There is no kind that means "parenthesised group that does not end the item".

Options:

1. Keep it a function and record it as a permanent exception. This is the current
   state.
2. Add such a kind. It is a rule with no counterpart in SQL, so it breaks the second
   golden rule.
3. Drop `DISTINCT_ON` and have users write `Kw("DISTINCT ON (...)")`. Consistent, but
   it gives up `Lit` inside the group.

## 2. `EXCLUDED` consumes the token after it

`EXCLUDED, UsersName` renders `EXCLUDED.name`. Two tokens produce one operand, and
the qualifier is rewritten. SQL has no such rule: `EXCLUDED.name` is an ordinary
qualified name, so this breaks the second golden rule and, because the DSL text and
the SQL text differ, the first one as well.

The rule-conforming spelling is an `Id` constant per column, or `Id("EXCLUDED.name")`
at the point of use. Whether that is worth the loss of convenience is undecided.

## 3. The operator names are not SQL spellings

`EQ`, `NE`, `GT`, `GTE`, `LT`, `LTE` are not how the operators are written in SQL, so
`UsersAge, GTE, Lit(18)` does not read as `users.age >= $1`. This is a deviation from
the first golden rule.

Go identifiers cannot be `=` or `>=`, so the only alternative is `Op(">=")`
everywhere. The constraint comes from Go, not from a design choice here, which is why
the constants stay for now.

## 4. Commas are inserted by the walker, not written by the user

The third golden rule makes parentheses explicit, but separators are not: the walker
decides where the commas go. Making them explicit would mean writing a token for each
one, which Go's type system gives no way to omit where a token renders empty. An
optional token that renders nothing would leave the comma next to it behind.

This is accepted for now, and it is the whole purpose of the package, but it is
recorded here because it is the one place where the reasoning behind rule 3 is not
applied to a separator.
