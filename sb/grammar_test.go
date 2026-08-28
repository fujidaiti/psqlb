package sb_test

import (
	"strings"
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
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
	stmt []any
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

// from_item: table | (select) AS alias | function(...) [AS alias]
func TestFromList(t *testing.T) {
	run(t, []gcase{
		{
			name: "table",
			stmt: stmt(SELECT, "*", FROM, Users),
			want: "SELECT * FROM users",
		},
		{
			name: "table with alias",
			stmt: stmt(SELECT, "*", FROM, Users, AS, sb.I("u")),
			want: "SELECT * FROM users AS u",
		},
		{
			name: "several tables",
			stmt: stmt(SELECT, "*", FROM, Users, Orders),
			want: "SELECT * FROM users, orders",
		},
		{
			name: "subquery with alias",
			stmt: stmt(SELECT, "*", FROM, sb.P(SELECT, UsersID, FROM, Users), AS, sb.I("t")),
			want: "SELECT * FROM (SELECT users.id FROM users) AS t",
		},
		{
			// A function in FROM needs no alias, so it is not asked for one.
			name: "function call",
			stmt: stmt(SELECT, "*", FROM, sb.F("unnest", sb.I("a"))),
			want: "SELECT * FROM unnest(a)",
		},
		{
			name: "function call with alias",
			stmt: stmt(SELECT, "*", FROM, sb.F("generate_series", 1, 10), AS, sb.I("g")),
			want: "SELECT * FROM generate_series($1, $2) AS g",
			args: []any{1, 10},
		},
	})
}

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

// insert: INSERT INTO table [(columns)] {VALUES (...) [, ...] | query}
//
//	[ON CONFLICT ...] [RETURNING ...]
func TestInsert(t *testing.T) {
	run(t, []gcase{
		{
			name: "one row",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P("a", "b")),
			want: "INSERT INTO users VALUES ($1, $2)",
			args: []any{"a", "b"},
		},
		{
			name: "named columns and several rows",
			stmt: stmt(INSERT, INTO, Users, sb.P(sb.I("name"), sb.I("email")),
				VALUES, sb.P("a", "b"), sb.P("c", DEFAULT)),
			want: "INSERT INTO users (name, email) VALUES ($1, $2), ($3, DEFAULT)",
			args: []any{"a", "b", "c"},
		},
		{
			// The query is not parenthesised, so it is written in place.
			name: "from a query",
			stmt: stmt(INSERT, INTO, Users, sb.P(sb.I("id")),
				SELECT, OrdersUserID, FROM, Orders),
			want: "INSERT INTO users (id) SELECT orders.user_id FROM orders",
		},
		{
			name: "DO NOTHING",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P("a"),
				ON, CONFLICT, DO, NOTHING),
			want: "INSERT INTO users VALUES ($1) ON CONFLICT DO NOTHING",
			args: []any{"a"},
		},
		{
			// excluded.name is an ordinary qualified name. SET strips the
			// qualifier from the target on the left of the "=" only, so the
			// expression on the right keeps its own.
			name: "DO UPDATE with a condition",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P("a"),
				ON, CONFLICT, sb.P(sb.I("email")),
				DO, UPDATE, SET, UsersName, "=", sb.I("excluded.name"),
				WHERE, UsersStatus, "<>", "banned",
				RETURNING, UsersID, AS, sb.I("i")),
			want: "INSERT INTO users VALUES ($1)" +
				" ON CONFLICT (email)" +
				" DO UPDATE SET name = excluded.name" +
				" WHERE users.status <> $2" +
				" RETURNING users.id AS i",
			args: []any{"a", "banned"},
		},
	})
}

// update: UPDATE table [AS alias] SET ... [FROM ...] [WHERE ...] [RETURNING ...]
func TestUpdate(t *testing.T) {
	run(t, []gcase{
		{
			name: "several assignments",
			stmt: stmt(UPDATE, Users, SET,
				UsersStatus, "=", "vip",
				UsersName, "=", sb.F("lower", UsersName),
				WHERE, UsersID, "=", 1),
			want: "UPDATE users SET status = $1, name = lower(users.name) WHERE users.id = $2",
			args: []any{"vip", 1},
		},
		{
			// The multi-column form is supported, and its targets are
			// unqualified for the same reason the single-column form's are.
			name: "the multi-column form",
			stmt: stmt(UPDATE, Users, SET,
				sb.P(UsersName, UsersStatus), "=", sb.P("a", "b")),
			want: "UPDATE users SET (name, status) = ($1, $2)",
			args: []any{"a", "b"},
		},
		{
			name: "aliased table and RETURNING",
			stmt: stmt(UPDATE, Users, AS, sb.I("u"), SET, UsersStatus, "=", DEFAULT,
				RETURNING, "*"),
			want: "UPDATE users AS u SET status = DEFAULT RETURNING *",
		},
	})
}

// delete: DELETE FROM table [AS alias] [USING ...] [WHERE ...] [RETURNING ...]
func TestDelete(t *testing.T) {
	run(t, []gcase{
		{
			name: "with a condition",
			stmt: stmt(DELETE, FROM, Users, WHERE, UsersID, "=", 1),
			want: "DELETE FROM users WHERE users.id = $1",
			args: []any{1},
		},
		{
			name: "USING and RETURNING",
			stmt: stmt(DELETE, FROM, Users, USING, Orders,
				WHERE, OrdersUserID, "=", UsersID,
				RETURNING, UsersID),
			want: "DELETE FROM users USING orders" +
				" WHERE orders.user_id = users.id" +
				" RETURNING users.id",
		},
	})
}

// joined_table: from_item [NATURAL] join_type from_item [ON ... | USING (...)]
func TestJoins(t *testing.T) {
	run(t, []gcase{
		{
			name: "inner join with ON",
			stmt: stmt(SELECT, "*", FROM, Users, JOIN, Orders, ON, OrdersUserID, "=", UsersID),
			want: "SELECT * FROM users JOIN orders ON orders.user_id = users.id",
		},
		{
			name: "outer join",
			stmt: stmt(SELECT, "*", FROM, Users, LEFT, OUTER, JOIN, Orders, ON, TRUE),
			want: "SELECT * FROM users LEFT OUTER JOIN orders ON TRUE",
		},
		{
			// OUTER is optional, and optional words combine rather than
			// multiplying into separate constants.
			name: "outer join without OUTER",
			stmt: stmt(SELECT, "*", FROM, Users, FULL, JOIN, Orders, ON, TRUE),
			want: "SELECT * FROM users FULL JOIN orders ON TRUE",
		},
		{
			name: "USING",
			stmt: stmt(SELECT, "*", FROM, Users, INNER, JOIN, Orders, USING, sb.P(sb.I("user_id"))),
			want: "SELECT * FROM users INNER JOIN orders USING (user_id)",
		},
		{
			name: "cross join takes no condition",
			stmt: stmt(SELECT, "*", FROM, Users, CROSS, JOIN, Orders),
			want: "SELECT * FROM users CROSS JOIN orders",
		},
		{
			name: "natural join",
			stmt: stmt(SELECT, "*", FROM, Users, NATURAL, LEFT, JOIN, Orders),
			want: "SELECT * FROM users NATURAL LEFT JOIN orders",
		},
		{
			name: "a chain of joins",
			stmt: stmt(SELECT, "*", FROM, Users,
				JOIN, Orders, ON, OrdersUserID, "=", UsersID,
				LEFT, JOIN, sb.I("items"), ON, sb.I("items.order_id"), "=", OrdersID),
			want: "SELECT * FROM users" +
				" JOIN orders ON orders.user_id = users.id" +
				" LEFT JOIN items ON items.order_id = orders.id",
		},
		{
			name: "join to a subquery",
			stmt: stmt(SELECT, "*", FROM, Users,
				JOIN, sb.P(SELECT, OrdersUserID, FROM, Orders), AS, sb.I("o"),
				ON, sb.I("o.user_id"), "=", UsersID),
			want: "SELECT * FROM users" +
				" JOIN (SELECT orders.user_id FROM orders) AS o" +
				" ON o.user_id = users.id",
		},
		{
			name: "LATERAL",
			stmt: stmt(SELECT, "*", FROM, Users,
				JOIN, LATERAL, sb.F("unnest", UsersMeta), AS, sb.I("m"), ON, TRUE),
			want: "SELECT * FROM users JOIN LATERAL unnest(users.meta) AS m ON TRUE",
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

// window functions: FILTER, OVER, the WINDOW clause and frame clauses.
func TestWindowFunctions(t *testing.T) {
	run(t, []gcase{
		{
			// OVER accepts a window name or a parenthesised definition. The
			// production is reached only where a window specification belongs,
			// so it can look at what is written and take either.
			name: "OVER a window name",
			stmt: stmt(SELECT, sb.F("SUM", OrdersTotal), OVER, sb.I("w"), FROM, Orders,
				WINDOW, sb.I("w"), AS, sb.P(PARTITION, BY, OrdersUserID)),
			want: "SELECT SUM(orders.total) OVER w FROM orders" +
				" WINDOW w AS (PARTITION BY orders.user_id)",
		},
		{
			name: "OVER a definition",
			stmt: stmt(SELECT, sb.F("row_number"), OVER, sb.P(ORDER, BY, OrdersID, DESC), FROM, Orders),
			want: "SELECT row_number() OVER (ORDER BY orders.id DESC) FROM orders",
		},
		{
			name: "OVER an empty definition",
			stmt: stmt(SELECT, sb.F("COUNT", "*"), OVER, sb.P(), FROM, Orders),
			want: "SELECT COUNT(*) OVER () FROM orders",
		},
		{
			name: "a frame with one bound",
			stmt: stmt(SELECT, sb.F("SUM", OrdersTotal), OVER, sb.P(
				ORDER, BY, OrdersID, RANGE, UNBOUNDED, PRECEDING), FROM, Orders),
			want: "SELECT SUM(orders.total) OVER" +
				" (ORDER BY orders.id RANGE UNBOUNDED PRECEDING) FROM orders",
		},
		{
			name: "a frame with an offset bound",
			stmt: stmt(SELECT, sb.F("SUM", OrdersTotal), OVER, sb.P(
				ORDER, BY, OrdersID,
				ROWS, BETWEEN, 3, PRECEDING, AND, 1, FOLLOWING), FROM, Orders),
			want: "SELECT SUM(orders.total) OVER" +
				" (ORDER BY orders.id ROWS BETWEEN $1 PRECEDING AND $2 FOLLOWING) FROM orders",
			args: []any{3, 1},
		},
		{
			name: "FILTER",
			stmt: stmt(SELECT, sb.F("COUNT", "*"), FILTER, sb.P(WHERE, UsersIsPaid), FROM, Users),
			want: "SELECT COUNT(*) FILTER (WHERE users.paid) FROM users",
		},
		{
			name: "FILTER and OVER together",
			stmt: stmt(SELECT,
				sb.F("COUNT", "*"), FILTER, sb.P(WHERE, UsersIsPaid), OVER, sb.I("w"), AS, sb.I("c"),
				FROM, Users, WINDOW, sb.I("w"), AS, sb.P()),
			want: "SELECT COUNT(*) FILTER (WHERE users.paid) OVER w AS c" +
				" FROM users WINDOW w AS ()",
		},
		{
			// An aggregate may order its input, which is why a function's
			// arguments are not simply an expression list.
			name: "ORDER BY inside an aggregate",
			stmt: stmt(SELECT, sb.F("string_agg", UsersName, ",", ORDER, BY, UsersID), FROM, Users),
			want: "SELECT string_agg(users.name, $1 ORDER BY users.id) FROM users",
			args: []any{","},
		},
	})
}

// with_query: WITH [RECURSIVE] name [(columns)] AS [[NOT] MATERIALIZED] (query)
func TestCTEs(t *testing.T) {
	body := sb.P(SELECT, UsersID, FROM, Users)
	run(t, []gcase{
		{
			name: "one query",
			stmt: stmt(WITH, sb.I("t"), AS, body, SELECT, "*", FROM, sb.I("t")),
			want: "WITH t AS (SELECT users.id FROM users) SELECT * FROM t",
		},
		{
			name: "several queries",
			stmt: stmt(WITH, sb.I("a"), AS, body, sb.I("b"), AS, body,
				SELECT, "*", FROM, sb.I("a")),
			want: "WITH a AS (SELECT users.id FROM users)," +
				" b AS (SELECT users.id FROM users)" +
				" SELECT * FROM a",
		},
		{
			name: "named columns and MATERIALIZED",
			stmt: stmt(WITH, sb.I("t"), sb.P(sb.I("id")), AS, MATERIALIZED, body,
				SELECT, "*", FROM, sb.I("t")),
			want: "WITH t (id) AS MATERIALIZED (SELECT users.id FROM users) SELECT * FROM t",
		},
		{
			name: "NOT MATERIALIZED",
			stmt: stmt(WITH, sb.I("t"), AS, NOT, MATERIALIZED, body, SELECT, "*", FROM, sb.I("t")),
			want: "WITH t AS NOT MATERIALIZED (SELECT users.id FROM users) SELECT * FROM t",
		},
		{
			name: "in front of a write statement",
			stmt: stmt(WITH, sb.I("t"), AS, body,
				DELETE, FROM, Users, WHERE, UsersID, IN, sb.P(SELECT, "*", FROM, sb.I("t"))),
			want: "WITH t AS (SELECT users.id FROM users)" +
				" DELETE FROM users WHERE users.id IN (SELECT * FROM t)",
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

// ===========================================================================
// Normalization: which plain Go values become operators
// ===========================================================================

// A string is an operator when it is a well-formed PostgreSQL operator name,
// and a value to bind otherwise. The rule is the one PostgreSQL's own lexer
// uses (reference manual 4.1.3), so no list of operators is kept and an
// extension operator needs nothing special.
//
// The two tables together are the pass-through invariant: the first is every
// shape of string that reaches the SQL text, and the second proves that a
// keyword-shaped or identifier-shaped string is bound instead of emitted.
func TestOperatorNames(t *testing.T) {
	// Emitted verbatim, binding nothing.
	for _, op := range []string{
		"=", "<>", "!=", ">=", "<=", ">", "<",
		"@>", "<@", "<->", "->>", "#>>", "||", "~", "!~*",
		"+", "-", "*", "/", "%",
		"@-", "#-", // a trailing "-" is allowed with a special character present
		strings.Repeat("@", 63), // the length cap, NAMEDATALEN-1
	} {
		t.Run("operator "+op, func(t *testing.T) {
			assertSQL(t,
				stmt(SELECT, "*", FROM, Users, WHERE, UsersID, op, UsersName),
				"SELECT * FROM users WHERE users.id "+op+" users.name",
			)
		})
	}

	// "::" is neither: ":" is not an operator character, so it cannot come
	// through the rule, and normalization maps it to the typecast keyword.
	t.Run(`typecast "::"`, func(t *testing.T) {
		assertSQL(t,
			stmt(SELECT, UsersID, FROM, Users, WHERE, UsersMeta, "@>", `{}`, "::", sb.I("jsonb")),
			"SELECT users.id FROM users WHERE users.meta @> $1::jsonb",
			`{}`,
		)
	})

	// Bound as a parameter. Each one fails the rule for a different reason.
	for _, name := range []string{
		"",                 // would vanish from the output: emitter.word drops it
		"--", "-- x", "/*", // either sequence opens a comment
		"<-", "=-", // a trailing "-" with no special character
		strings.Repeat("@", 64), // one over the length cap
		",", "(", ")",           // punctuation that is not an operator character
		"a%", "NULL", "id", // a character outside the set
		"users.id", "1", "count(*)",
	} {
		t.Run("value "+name, func(t *testing.T) {
			assertSQL(t,
				stmt(SELECT, "*", FROM, Users, WHERE, UsersID, "=", name),
				"SELECT * FROM users WHERE users.id = $1",
				name,
			)
		})
	}
}
