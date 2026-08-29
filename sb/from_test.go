package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

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

// PostgreSQL requires an alias on a subquery in FROM, and requires it to be
// written after the subquery. Both halves are reported here rather than by the
// server. Nothing else about aliases is guessed: see TestSelectList.
func TestSubqueryAlias(t *testing.T) {
	sub := sb.P(SELECT, UsersID, FROM, Users)
	runErrs(t, []ecase{
		{
			name: "no alias",
			stmt: stmt(SELECT, "*", FROM, sub),
			want: "an alias on a subquery is required",
		},
		{
			name: "alias without AS",
			stmt: stmt(SELECT, "*", FROM, sub, sb.I("t")),
			want: `write sb.P(...), AS, sb.I("t")`,
		},
	})
}

// A join that PostgreSQL requires a condition on is not emitted without one,
// and one that takes no condition is not emitted with one.
func TestJoinConditions(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "no condition",
			stmt: stmt(SELECT, "*", FROM, Users, JOIN, Orders),
			want: "JOIN: a join condition is required",
		},
		{
			name: "a condition on a CROSS join",
			stmt: stmt(SELECT, "*", FROM, Users, CROSS, JOIN, Orders, ON, TRUE),
			want: "expected no condition, since a CROSS or NATURAL join takes none",
		},
		{
			name: "USING without a group",
			stmt: stmt(SELECT, "*", FROM, Users, JOIN, Orders, USING, sb.I("user_id")),
			want: "a parenthesised list of column names is required",
		},
		{
			name: "a join type with no JOIN",
			stmt: stmt(SELECT, "*", FROM, Users, LEFT, Orders),
			want: "JOIN: expected keyword JOIN",
		},
		{
			name: "LATERAL on a table",
			stmt: stmt(SELECT, "*", FROM, LATERAL, Users),
			want: "expected a subquery or a function call after LATERAL",
		},
	})
}

// A FROM item is a table, a subquery or a function call, and nothing else: a
// value has no meaning in that position and is not bound there.
func TestFromItemErrors(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "a table position that takes no expression",
			stmt: stmt(SELECT, "*", FROM, 1),
			want: "FROM: expected a table name, a subquery or a function call",
		},
	})
}
