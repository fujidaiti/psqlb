package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// condition: the expression productions, reached through WHERE.
func TestExpressions(t *testing.T) {
	where := func(items ...any) []any {
		return append(stmt(SELECT, "*", FROM, Users, WHERE), items...)
	}
	const head = "SELECT * FROM users WHERE "

	run(t, []gcase{
		{
			name: "named operator",
			stmt: where(UsersAge, ">=", 18),
			want: head + "users.age >= $1",
			args: []any{18},
		},
		{
			name: "hand-written operator",
			stmt: where(UsersMeta, "@>", "{}"),
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
			stmt: where(UsersStatus, IS, NOT, DISTINCT, FROM, "x"),
			want: head + "users.status IS NOT DISTINCT FROM $1",
			args: []any{"x"},
		},
		{
			name: "IN a list",
			stmt: where(UsersStatus, IN, sb.P("a", "b")),
			want: head + "users.status IN ($1, $2)",
			args: []any{"a", "b"},
		},
		{
			name: "IN a subquery",
			stmt: where(UsersID, IN, sb.P(SELECT, OrdersUserID, FROM, Orders)),
			want: head + "users.id IN (SELECT orders.user_id FROM orders)",
		},
		{
			name: "NOT IN",
			stmt: where(UsersStatus, NOT, IN, sb.P("a")),
			want: head + "users.status NOT IN ($1)",
			args: []any{"a"},
		},
		{
			name: "BETWEEN",
			stmt: where(UsersAge, BETWEEN, 13, AND, 19),
			want: head + "users.age BETWEEN $1 AND $2",
			args: []any{13, 19},
		},
		{
			// The AND belongs to BETWEEN, so the one after it is still the
			// boolean operator.
			name: "NOT BETWEEN, then AND",
			stmt: where(UsersAge, NOT, BETWEEN, 13, AND, 19, AND, UsersIsPaid),
			want: head + "users.age NOT BETWEEN $1 AND $2 AND users.paid",
			args: []any{13, 19},
		},
		{
			name: "LIKE and NOT LIKE",
			stmt: where(UsersName, LIKE, "a%", AND, UsersName, NOT, ILIKE, "b%"),
			want: head + "users.name LIKE $1 AND users.name NOT ILIKE $2",
			args: []any{"a%", "b%"},
		},
		{
			name: "EXISTS",
			stmt: where(EXISTS, sb.P(SELECT, 1, FROM, Orders)),
			want: head + "EXISTS (SELECT $1 FROM orders)",
			args: []any{1},
		},
		{
			name: "quantified comparison",
			stmt: where(UsersID, "=", ANY, sb.P(SELECT, OrdersUserID, FROM, Orders)),
			want: head + "users.id = ANY (SELECT orders.user_id FROM orders)",
		},
		{
			name: "grouped condition",
			stmt: where(UsersIsPaid, AND, sb.P(UsersHasTicket, OR, UsersIsPaid)),
			want: head + "users.paid AND (users.has_ticket OR users.paid)",
		},
		{
			name: "row constructor",
			stmt: where(sb.P(UsersID, UsersAge), "=", sb.P(1, 2)),
			want: head + "(users.id, users.age) = ($1, $2)",
			args: []any{1, 2},
		},
		{
			// One more level of nesting is one more pair of parentheses. This
			// compares against a one-element list holding a scalar subquery,
			// which is a different query from the membership test above.
			name: "a nested group keeps its parentheses",
			stmt: where(UsersID, IN, sb.P(sb.P(SELECT, OrdersUserID, FROM, Orders))),
			want: head + "users.id IN ((SELECT orders.user_id FROM orders))",
		},
		{
			name: "typecast",
			stmt: where(UsersMeta, "::", sb.I("jsonb"), "@>", "{}"),
			want: head + "users.meta::jsonb @> $1",
			args: []any{"{}"},
		},
		{
			name: "scalar subquery",
			stmt: where(UsersAge, ">", sb.P(SELECT, sb.F("avg", UsersAge), FROM, Users)),
			want: head + "users.age > (SELECT avg(users.age) FROM users)",
		},
		{
			name: "function call",
			stmt: where(sb.F("lower", UsersName), "=", "a"),
			want: head + "lower(users.name) = $1",
			args: []any{"a"},
		},
		{
			name: "function call with DISTINCT",
			stmt: where(sb.F("COUNT", DISTINCT, UsersID), ">", 1),
			want: head + "COUNT(DISTINCT users.id) > $1",
			args: []any{1},
		},
		{
			name: "function call with no arguments",
			stmt: where(UsersCreated, "<", sb.F("now")),
			want: head + "users.created_at < now()",
		},
		{
			name: "CASE with an operand",
			stmt: where(sb.P(CASE, UsersStatus, WHEN, "a", THEN, 1, ELSE, 0, END), "=", 1),
			want: head + "(CASE users.status WHEN $1 THEN $2 ELSE $3 END) = $4",
			args: []any{"a", 1, 0, 1},
		},
	})
}

// The expression forms that landed last.
func TestRemainingExpressions(t *testing.T) {
	run(t, []gcase{
		{
			name: "COLLATE",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, BY, UsersName, COLLATE, sb.I(`"C"`), DESC),
			want: `SELECT users.id FROM users ORDER BY users.name COLLATE "C" DESC`,
		},
		{
			name: "SIMILAR TO",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersName, SIMILAR, TO, "%a%"),
			want: "SELECT users.id FROM users WHERE users.name SIMILAR TO $1",
			args: []any{"%a%"},
		},
		{
			name: "NOT SIMILAR TO",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersName, NOT, SIMILAR, TO, "%a%"),
			want: "SELECT users.id FROM users WHERE users.name NOT SIMILAR TO $1",
			args: []any{"%a%"},
		},
		{
			name: "a bare VALUES query",
			stmt: stmt(VALUES, sb.P(1, "a"), sb.P(2, "b")),
			want: "VALUES ($1, $2), ($3, $4)",
			args: []any{1, "a", 2, "b"},
		},
	})
}

// Shape is validated: an operand, then an operator, then an operand. Two
// operands in a row are an error wherever the position takes exactly one
// expression.
func TestExpressionShape(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "two operands in WHERE",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, UsersName),
			want: "expected the end of the statement",
		},
		{
			name: "a dangling operator",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, "="),
			want: "WHERE: expected an expression",
		},
		{
			name: "a dangling AND",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersIsPaid, AND),
			want: "WHERE: expected an expression",
		},
	})
}

// A position that SQL always parenthesises requires a group, so the keyword that
// needs one can be a constant like any other.
func TestMissingGroup(t *testing.T) {
	runErrs(t, []ecase{
		{
			// The old engine produced "IN $1, $2", which Postgres rejects.
			name: "IN without a group",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersStatus, IN, 1, 2),
			want: "a parenthesised list or subquery after IN is required",
		},
		{
			name: "EXISTS without a group",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, EXISTS, 1),
			want: "a parenthesised subquery after EXISTS is required",
		},
		{
			name: "ANY without a group",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, "=", ANY, UsersName),
			want: "a parenthesised subquery or list after ANY is required",
		},
	})
}

// A keyword phrase that is written wrong is reported, naming the word that was
// expected. This is what word-level keywords cost and what the parser gives back:
// the phrase is checked rather than being one opaque constant.
func TestExpressionKeywordPhrases(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "IS followed by nothing it accepts",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, IS, 1),
			want: "expected keyword NULL, TRUE, FALSE, UNKNOWN or DISTINCT",
		},
		{
			name: "BETWEEN without AND",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersAge, BETWEEN, 1, 2),
			want: "expected keyword AND",
		},
		{
			name: "WHEN without THEN",
			stmt: stmt(SELECT, CASE, WHEN, UsersIsPaid, 1, END),
			want: "CASE: expected keyword THEN",
		},
		{
			name: "CASE without END",
			stmt: stmt(SELECT, CASE, WHEN, UsersIsPaid, THEN, 1),
			want: "CASE: expected keyword END",
		},
		{
			name: "COLLATE without a collation name",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, BY, UsersName, COLLATE, "C"),
			want: "expected a collation name written with sb.I",
		},
		{
			name: "a type name that is not an identifier",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersMeta, "::", 1),
			want: "expected a type name written with sb.I or sb.RawExpr",
		},
	})
}
