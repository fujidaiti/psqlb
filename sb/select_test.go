package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// select: SELECT [ALL | DISTINCT [ON (...)]] expression [AS name] [, ...]
func TestSelectList(t *testing.T) {
	run(t, []gcase{
		{
			name: "one item",
			stmt: stmt(SELECT, UsersID),
			want: "SELECT users.id",
		},
		{
			name: "several items",
			stmt: stmt(SELECT, UsersID, UsersName, UsersEmail),
			want: "SELECT users.id, users.name, users.email",
		},
		{
			name: "star",
			stmt: stmt(SELECT, "*"),
			want: "SELECT *",
		},
		{
			name: "alias",
			stmt: stmt(SELECT, UsersID, AS, sb.I("i"), UsersName, AS, sb.I("n")),
			want: "SELECT users.id AS i, users.name AS n",
		},
		{
			// An identifier standing on its own is the next item, never an
			// alias for the one before it. SQL allows the implicit form and the
			// DSL does not, because the two are the same token sequence.
			name: "a bare name is the next item, not an alias",
			stmt: stmt(SELECT, UsersID, sb.I("x")),
			want: "SELECT users.id, x",
		},
		{
			name: "ALL",
			stmt: stmt(SELECT, ALL, UsersID),
			want: "SELECT ALL users.id",
		},
		{
			name: "DISTINCT",
			stmt: stmt(SELECT, DISTINCT, UsersID),
			want: "SELECT DISTINCT users.id",
		},
		{
			name: "DISTINCT ON with several keys",
			stmt: stmt(SELECT, DISTINCT, ON, sb.P(UsersID, UsersName), UsersID),
			want: "SELECT DISTINCT ON (users.id, users.name) users.id",
		},
		{
			name: "an expression as an item",
			stmt: stmt(SELECT, UsersAge, ">=", 18, AS, sb.I("adult"), UsersName),
			want: "SELECT users.age >= $1 AS adult, users.name",
			args: []any{18},
		},
	})
}

// sort_key: expression [ASC | DESC] [NULLS {FIRST | LAST}]
func TestOrderLimitOffset(t *testing.T) {
	run(t, []gcase{
		{
			name: "one key",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID),
			want: "SELECT users.id FROM users ORDER BY users.id",
		},
		{
			name: "every modifier",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, BY,
				UsersID, ASC, NULLS, FIRST, UsersName, DESC, NULLS, LAST, UsersEmail),
			want: "SELECT users.id FROM users" +
				" ORDER BY users.id ASC NULLS FIRST, users.name DESC NULLS LAST, users.email",
		},
		{
			name: "an expression as a key",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, BY, sb.F("lower", UsersName), DESC),
			want: "SELECT users.id FROM users ORDER BY lower(users.name) DESC",
		},
		{
			name: "LIMIT and OFFSET",
			stmt: stmt(SELECT, UsersID, FROM, Users, LIMIT, 10, OFFSET, 20),
			want: "SELECT users.id FROM users LIMIT $1 OFFSET $2",
			args: []any{10, 20},
		},
		{
			name: "LIMIT ALL",
			stmt: stmt(SELECT, UsersID, FROM, Users, LIMIT, ALL),
			want: "SELECT users.id FROM users LIMIT ALL",
		},
	})
}

// group_by: GROUP BY grouping_element [, ...] [HAVING condition]
func TestGroupBy(t *testing.T) {
	run(t, []gcase{
		{
			name: "several keys",
			stmt: stmt(SELECT, UsersID, FROM, Users, GROUP, BY, UsersID, UsersName),
			want: "SELECT users.id FROM users GROUP BY users.id, users.name",
		},
		{
			name: "HAVING",
			stmt: stmt(SELECT, UsersID, sb.F("COUNT", "*"), FROM, Users,
				GROUP, BY, UsersID,
				HAVING, sb.F("COUNT", "*"), ">", 1),
			want: "SELECT users.id, COUNT(*) FROM users" +
				" GROUP BY users.id" +
				" HAVING COUNT(*) > $1",
			args: []any{1},
		},
		{
			// ROLLUP and CUBE are function-call syntax, so they need nothing of
			// their own.
			name: "ROLLUP",
			stmt: stmt(SELECT, UsersID, FROM, Users, GROUP, BY, sb.F("ROLLUP", UsersID, UsersName)),
			want: "SELECT users.id FROM users GROUP BY ROLLUP(users.id, users.name)",
		},
		{
			name: "every clause in order",
			stmt: stmt(SELECT, UsersID, FROM, Users,
				WHERE, UsersIsPaid,
				GROUP, BY, UsersID,
				HAVING, sb.F("COUNT", "*"), ">", 1,
				ORDER, BY, UsersID,
				LIMIT, 10, OFFSET, 5),
			want: "SELECT users.id FROM users" +
				" WHERE users.paid" +
				" GROUP BY users.id" +
				" HAVING COUNT(*) > $1" +
				" ORDER BY users.id" +
				" LIMIT $2 OFFSET $3",
			args: []any{1, 10, 5},
		},
	})
}

// query: select_term [{UNION | INTERSECT | EXCEPT} [ALL | DISTINCT] select_term]
func TestSetOperations(t *testing.T) {
	run(t, []gcase{
		{
			name: "UNION",
			stmt: stmt(SELECT, UsersID, FROM, Users, UNION, SELECT, OrdersUserID, FROM, Orders),
			want: "SELECT users.id FROM users UNION SELECT orders.user_id FROM orders",
		},
		{
			name: "EXCEPT ALL",
			stmt: stmt(SELECT, UsersID, FROM, Users, EXCEPT, ALL, SELECT, OrdersUserID, FROM, Orders),
			want: "SELECT users.id FROM users EXCEPT ALL SELECT orders.user_id FROM orders",
		},
		{
			name: "three terms",
			stmt: stmt(SELECT, UsersID, FROM, Users,
				INTERSECT, SELECT, OrdersUserID, FROM, Orders,
				UNION, SELECT, 1),
			want: "SELECT users.id FROM users" +
				" INTERSECT SELECT orders.user_id FROM orders" +
				" UNION SELECT $1",
			args: []any{1},
		},
	})
}

// The select list rejects what cannot begin an item. Nothing about an alias is
// guessed: an identifier standing on its own is the next item, so an alias is
// written with AS and must be an sb.I. See TestSelectList.
func TestSelectListErrors(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "an empty select list",
			stmt: stmt(SELECT),
			want: "SELECT: expected an expression",
		},
		{
			name: "a keyword where an expression belongs",
			stmt: stmt(SELECT, FROM, Users),
			want: "SELECT: expected an expression, got keyword FROM",
		},
		{
			name: "an alias that is not an identifier",
			stmt: stmt(SELECT, UsersID, AS, "x"),
			want: "expected an alias written with sb.I",
		},
		{
			// A position that SQL always parenthesises requires a group, so the
			// keyword that needs one can be a constant like any other.
			name: "DISTINCT ON without a group",
			stmt: stmt(SELECT, DISTINCT, ON, UsersID, UsersID),
			want: "a parenthesised list of expressions is required",
		},
	})
}

// A keyword phrase that is written wrong is reported, naming the word that was
// expected. This is what word-level keywords cost and what the parser gives back:
// the phrase is checked rather than being one opaque constant.
func TestOrderByErrors(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "ORDER without BY",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, UsersID),
			want: "ORDER BY: expected keyword BY",
		},
		{
			name: "NULLS without FIRST or LAST",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID, NULLS, DESC),
			want: "expected keyword FIRST or LAST",
		},
	})
}

func TestOrderByUsingIsNotSupported(t *testing.T) {
	assertUnsupported(t,
		stmt(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID, USING, ">"),
		"ORDER BY ... USING")
}

// A set operation joins two query terms, so the second one has to be there.
func TestSetOperationErrors(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "a set operation with nothing after it",
			stmt: stmt(SELECT, UsersID, FROM, Users, UNION, ALL),
			want: "statement: expected keyword SELECT",
		},
	})
}
