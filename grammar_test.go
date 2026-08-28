package psqlb

import (
	"testing"

	"github.com/fujidaiti/psqlb/sb"
)

// ===========================================================================
// Grammar coverage
// ===========================================================================
//
// sql_test.go is the usage documentation: whole statements, written the way a
// user writes them. This file is the mechanical coverage of the grammar,
// organised by production rather than by example: one group of cases per
// production, covering each optional element and each branch.

type gcase struct {
	name string
	stmt sb.Group
	want string
	args []any
}

func run(t *testing.T, cases []gcase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertSQL(t, c.stmt, c.want, c.args...)
		})
	}
}

// select: SELECT [ALL | DISTINCT [ON (...)]] expression [AS name] [, ...]
func TestSelectList(t *testing.T) {
	run(t, []gcase{
		{
			name: "one item",
			stmt: sb.S(SELECT, UsersID),
			want: "SELECT users.id",
		},
		{
			name: "several items",
			stmt: sb.S(SELECT, UsersID, UsersName, UsersEmail),
			want: "SELECT users.id, users.name, users.email",
		},
		{
			name: "star",
			stmt: sb.S(SELECT, STAR),
			want: "SELECT *",
		},
		{
			name: "alias",
			stmt: sb.S(SELECT, UsersID, AS, sb.Id("i"), UsersName, AS, sb.Id("n")),
			want: "SELECT users.id AS i, users.name AS n",
		},
		{
			// An identifier standing on its own is the next item, never an
			// alias for the one before it. SQL allows the implicit form and the
			// DSL does not, because the two are the same token sequence.
			name: "a bare name is the next item, not an alias",
			stmt: sb.S(SELECT, UsersID, sb.Id("x")),
			want: "SELECT users.id, x",
		},
		{
			name: "ALL",
			stmt: sb.S(SELECT, ALL, UsersID),
			want: "SELECT ALL users.id",
		},
		{
			name: "DISTINCT",
			stmt: sb.S(SELECT, DISTINCT, UsersID),
			want: "SELECT DISTINCT users.id",
		},
		{
			name: "DISTINCT ON with several keys",
			stmt: sb.S(SELECT, DISTINCT, ON, sb.S(UsersID, UsersName), UsersID),
			want: "SELECT DISTINCT ON (users.id, users.name) users.id",
		},
		{
			name: "an expression as an item",
			stmt: sb.S(SELECT, UsersAge, GTE, sb.Lit(18), AS, sb.Id("adult"), UsersName),
			want: "SELECT users.age >= $1 AS adult, users.name",
			args: []any{18},
		},
	})
}

// from_item: table | (select) AS alias | function(...) [AS alias]
func TestFromList(t *testing.T) {
	run(t, []gcase{
		{
			name: "table",
			stmt: sb.S(SELECT, STAR, FROM, Users),
			want: "SELECT * FROM users",
		},
		{
			name: "table with alias",
			stmt: sb.S(SELECT, STAR, FROM, Users, AS, sb.Id("u")),
			want: "SELECT * FROM users AS u",
		},
		{
			name: "several tables",
			stmt: sb.S(SELECT, STAR, FROM, Users, Orders),
			want: "SELECT * FROM users, orders",
		},
		{
			name: "subquery with alias",
			stmt: sb.S(SELECT, STAR, FROM, sb.S(SELECT, UsersID, FROM, Users), AS, sb.Id("t")),
			want: "SELECT * FROM (SELECT users.id FROM users) AS t",
		},
		{
			// A function in FROM needs no alias, so it is not asked for one.
			name: "function call",
			stmt: sb.S(SELECT, STAR, FROM, sb.Func("unnest", sb.Id("a"))),
			want: "SELECT * FROM unnest(a)",
		},
		{
			name: "function call with alias",
			stmt: sb.S(SELECT, STAR, FROM, sb.Func("generate_series", sb.Lit(1), sb.Lit(10)), AS, sb.Id("g")),
			want: "SELECT * FROM generate_series($1, $2) AS g",
			args: []any{1, 10},
		},
	})
}

// condition: the expression productions, reached through WHERE.
func TestExpressions(t *testing.T) {
	where := func(items ...sb.Token) sb.Group {
		return sb.S(append([]sb.Token{SELECT, STAR, FROM, Users, WHERE}, items...)...)
	}
	const head = "SELECT * FROM users WHERE "

	run(t, []gcase{
		{
			name: "named operator",
			stmt: where(UsersAge, GTE, sb.Lit(18)),
			want: head + "users.age >= $1",
			args: []any{18},
		},
		{
			name: "hand-written operator",
			stmt: where(UsersMeta, sb.Op("@>"), sb.Lit("{}")),
			want: head + "users.meta @> $1",
			args: []any{"{}"},
		},
		{
			name: "AND and OR",
			stmt: where(UsersIsPaid, AND, UsersHasTicket, OR, UsersIsPaid),
			want: head + "users.paid AND users.has_ticket OR users.paid",
		},
		{
			name: "NOT",
			stmt: where(NOT, UsersIsPaid),
			want: head + "NOT users.paid",
		},
		{
			name: "IS NULL",
			stmt: where(UsersEmail, IS, NULL),
			want: head + "users.email IS NULL",
		},
		{
			name: "IS NOT NULL",
			stmt: where(UsersEmail, IS, NOT, NULL),
			want: head + "users.email IS NOT NULL",
		},
		{
			name: "IS NOT TRUE",
			stmt: where(UsersIsPaid, IS, NOT, TRUE),
			want: head + "users.paid IS NOT TRUE",
		},
		{
			name: "IS NOT DISTINCT FROM",
			stmt: where(UsersStatus, IS, NOT, DISTINCT, FROM, sb.Lit("x")),
			want: head + "users.status IS NOT DISTINCT FROM $1",
			args: []any{"x"},
		},
		{
			name: "IN a list",
			stmt: where(UsersStatus, IN, sb.S(sb.Lit("a"), sb.Lit("b"))),
			want: head + "users.status IN ($1, $2)",
			args: []any{"a", "b"},
		},
		{
			name: "IN a subquery",
			stmt: where(UsersID, IN, sb.S(SELECT, OrdersUserID, FROM, Orders)),
			want: head + "users.id IN (SELECT orders.user_id FROM orders)",
		},
		{
			name: "NOT IN",
			stmt: where(UsersStatus, NOT, IN, sb.S(sb.Lit("a"))),
			want: head + "users.status NOT IN ($1)",
			args: []any{"a"},
		},
		{
			name: "BETWEEN",
			stmt: where(UsersAge, BETWEEN, sb.Lit(13), AND, sb.Lit(19)),
			want: head + "users.age BETWEEN $1 AND $2",
			args: []any{13, 19},
		},
		{
			// The AND belongs to BETWEEN, so the one after it is still the
			// boolean operator.
			name: "NOT BETWEEN, then AND",
			stmt: where(UsersAge, NOT, BETWEEN, sb.Lit(13), AND, sb.Lit(19), AND, UsersIsPaid),
			want: head + "users.age NOT BETWEEN $1 AND $2 AND users.paid",
			args: []any{13, 19},
		},
		{
			name: "LIKE and NOT LIKE",
			stmt: where(UsersName, LIKE, sb.Lit("a%"), AND, UsersName, NOT, ILIKE, sb.Lit("b%")),
			want: head + "users.name LIKE $1 AND users.name NOT ILIKE $2",
			args: []any{"a%", "b%"},
		},
		{
			name: "EXISTS",
			stmt: where(EXISTS, sb.S(SELECT, sb.Lit(1), FROM, Orders)),
			want: head + "EXISTS (SELECT $1 FROM orders)",
			args: []any{1},
		},
		{
			name: "quantified comparison",
			stmt: where(UsersID, EQ, ANY, sb.S(SELECT, OrdersUserID, FROM, Orders)),
			want: head + "users.id = ANY (SELECT orders.user_id FROM orders)",
		},
		{
			name: "grouped condition",
			stmt: where(UsersIsPaid, AND, sb.S(UsersHasTicket, OR, UsersIsPaid)),
			want: head + "users.paid AND (users.has_ticket OR users.paid)",
		},
		{
			name: "row constructor",
			stmt: where(sb.S(UsersID, UsersAge), EQ, sb.S(sb.Lit(1), sb.Lit(2))),
			want: head + "(users.id, users.age) = ($1, $2)",
			args: []any{1, 2},
		},
		{
			// One more level of nesting is one more pair of parentheses. This
			// compares against a one-element list holding a scalar subquery,
			// which is a different query from the membership test above.
			name: "a nested group keeps its parentheses",
			stmt: where(UsersID, IN, sb.S(sb.S(SELECT, OrdersUserID, FROM, Orders))),
			want: head + "users.id IN ((SELECT orders.user_id FROM orders))",
		},
		{
			name: "typecast",
			stmt: where(UsersMeta, TYPECAST, sb.Id("jsonb"), sb.Op("@>"), sb.Lit("{}")),
			want: head + "users.meta::jsonb @> $1",
			args: []any{"{}"},
		},
		{
			name: "scalar subquery",
			stmt: where(UsersAge, GT, sb.S(SELECT, sb.Func("avg", UsersAge), FROM, Users)),
			want: head + "users.age > (SELECT avg(users.age) FROM users)",
		},
		{
			name: "function call",
			stmt: where(sb.Func("lower", UsersName), EQ, sb.Lit("a")),
			want: head + "lower(users.name) = $1",
			args: []any{"a"},
		},
		{
			name: "function call with DISTINCT",
			stmt: where(sb.Func("COUNT", DISTINCT, UsersID), GT, sb.Lit(1)),
			want: head + "COUNT(DISTINCT users.id) > $1",
			args: []any{1},
		},
		{
			name: "function call with no arguments",
			stmt: where(UsersCreated, LT, sb.Func("now")),
			want: head + "users.created_at < now()",
		},
		{
			name: "CASE with an operand",
			stmt: where(sb.S(CASE, UsersStatus, WHEN, sb.Lit("a"), THEN, sb.Lit(1), ELSE, sb.Lit(0), END), EQ, sb.Lit(1)),
			want: head + "(CASE users.status WHEN $1 THEN $2 ELSE $3 END) = $4",
			args: []any{"a", 1, 0, 1},
		},
	})
}

// sort_key: expression [ASC | DESC] [NULLS {FIRST | LAST}]
func TestOrderLimitOffset(t *testing.T) {
	run(t, []gcase{
		{
			name: "one key",
			stmt: sb.S(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID),
			want: "SELECT users.id FROM users ORDER BY users.id",
		},
		{
			name: "every modifier",
			stmt: sb.S(SELECT, UsersID, FROM, Users, ORDER, BY,
				UsersID, ASC, NULLS, FIRST, UsersName, DESC, NULLS, LAST, UsersEmail),
			want: "SELECT users.id FROM users" +
				" ORDER BY users.id ASC NULLS FIRST, users.name DESC NULLS LAST, users.email",
		},
		{
			name: "an expression as a key",
			stmt: sb.S(SELECT, UsersID, FROM, Users, ORDER, BY, sb.Func("lower", UsersName), DESC),
			want: "SELECT users.id FROM users ORDER BY lower(users.name) DESC",
		},
		{
			name: "LIMIT and OFFSET",
			stmt: sb.S(SELECT, UsersID, FROM, Users, LIMIT, sb.Lit(10), OFFSET, sb.Lit(20)),
			want: "SELECT users.id FROM users LIMIT $1 OFFSET $2",
			args: []any{10, 20},
		},
		{
			name: "LIMIT ALL",
			stmt: sb.S(SELECT, UsersID, FROM, Users, LIMIT, ALL),
			want: "SELECT users.id FROM users LIMIT ALL",
		},
	})
}
