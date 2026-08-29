package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

func TestExampleQueries(t *testing.T) {
	run(t, []gcase{
		{
			name: "a condition list with a parenthesised OR",
			stmt: stmt(
				SELECT, UsersID, UsersName,
				FROM, Users,
				WHERE,
				UsersID, "=", 0, AND,
				UsersName, ">", "name", AND,
				sb.P(UsersIsPaid, OR, UsersHasTicket),
			),
			want: "SELECT users.id, users.name" +
				" FROM users" +
				" WHERE" +
				" users.id = $1 AND" +
				" users.name > $2 AND" +
				" (users.paid OR users.has_ticket)",
			args: []any{0, "name"},
		},
		{
			name: "a sort key keeps its own modifiers, so the comma falls between keys",
			stmt: stmt(
				SELECT, UsersID,
				FROM, Users,
				WHERE, sb.P(UsersCreated, UsersID), "<", sb.P("2025-06-01", 500),
				ORDER, BY, UsersCreated, DESC, UsersID, DESC, NULLS, LAST,
				LIMIT, 20,
			),
			want: "SELECT users.id" +
				" FROM users" +
				" WHERE (users.created_at, users.id) < ($1, $2)" +
				" ORDER BY users.created_at DESC, users.id DESC NULLS LAST" +
				" LIMIT $3",
			args: []any{"2025-06-01", 500, 20},
		},
		{
			name: "a nested sb.P lets a set-operation term carry ORDER BY and LIMIT",
			stmt: stmt(
				sb.P(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID, LIMIT, 10),
				UNION, ALL,
				sb.P(SELECT, OrdersUserID, FROM, Orders, ORDER, BY, OrdersID, LIMIT, 10),
			),
			want: "(SELECT users.id FROM users ORDER BY users.id LIMIT $1)" +
				" UNION ALL" +
				" (SELECT orders.user_id FROM orders ORDER BY orders.id LIMIT $2)",
			args: []any{10, 10},
		},
		{
			name: "an operator symbol, IS DISTINCT FROM and IS NOT NULL",
			stmt: stmt(
				SELECT, UsersID,
				FROM, Users,
				WHERE,
				UsersMeta, "@>", sb.RawExpr(`'{"vip": true}'`), AND,
				UsersName, "~", "^a", AND,
				UsersStatus, IS, DISTINCT, FROM, "banned", AND,
				UsersEmail, IS, NOT, NULL,
			),
			want: `SELECT users.id` +
				` FROM users` +
				` WHERE` +
				` users.meta @> '{"vip": true}' AND` +
				` users.name ~ $1 AND` +
				` users.status IS DISTINCT FROM $2 AND` +
				` users.email IS NOT NULL`,
			args: []any{"^a", "banned"},
		},
		{
			name: "sb.RawExpr binds its own values at the $0 markers",
			stmt: stmt(
				SELECT, UsersID,
				FROM, Users,
				WHERE,
				sb.RawExpr("users.meta->'profile'->>'city' = $0", "Tokyo"), AND,
				sb.RawExpr("users.meta @> $0::jsonb", `{"vip": true}`), AND,
				UsersStatus, "=", "active",
			),
			want: "SELECT users.id" +
				" FROM users" +
				" WHERE" +
				" users.meta->'profile'->>'city' = $1 AND" +
				" users.meta @> $2::jsonb AND" +
				" users.status = $3",
			args: []any{"Tokyo", `{"vip": true}`, "active"},
		},
		{
			name: "a typecast written as tokens glued to the ::",
			stmt: stmt(
				SELECT, UsersID,
				FROM, Users,
				WHERE, UsersMeta, "@>", `{"vip": true}`, "::", sb.I("jsonb"),
			),
			want: "SELECT users.id" +
				" FROM users" +
				" WHERE users.meta @> $1::jsonb",
			args: []any{`{"vip": true}`},
		},
		{
			name: "DISTINCT ON takes a group without a comma after it",
			stmt: stmt(
				SELECT,
				DISTINCT, ON, sb.P(UsersID), UsersID,
				UsersName,
				FROM, Users,
				ORDER, BY, UsersID, UsersCreated, DESC,
			),
			want: "SELECT" +
				" DISTINCT ON (users.id) users.id," +
				" users.name" +
				" FROM users" +
				" ORDER BY users.id, users.created_at DESC",
		},
		{
			name: "FILTER and the alias stay in one select-list item",
			stmt: stmt(
				SELECT,
				UsersID,
				UsersName,
				sb.F("COUNT", "*"), FILTER, sb.P(WHERE, UsersIsPaid), AS, sb.I("paid_count"),
				FROM, Users,
				GROUP, BY, UsersID, UsersName,
			),
			want: "SELECT" +
				" users.id," +
				" users.name," +
				" COUNT(*) FILTER (WHERE users.paid) AS paid_count" +
				" FROM users" +
				" GROUP BY users.id, users.name",
		},
		{
			name: "a CASE expression is one select-list item, ending at END",
			stmt: stmt(
				SELECT,
				UsersID,
				CASE,
				WHEN, UsersAge, ">=", 18, THEN, "adult",
				WHEN, UsersAge, ">=", 13, THEN, "teen",
				ELSE, "child",
				END, AS, sb.I("bucket"),
				FROM, Users,
			),
			want: "SELECT" +
				" users.id," +
				" CASE" +
				" WHEN users.age >= $1 THEN $2" +
				" WHEN users.age >= $3 THEN $4" +
				" ELSE $5" +
				" END AS bucket" +
				" FROM users",
			args: []any{18, "adult", 13, "teen", "child"},
		},
		{
			name: "ON CONFLICT DO UPDATE, where SET strips only the left-hand qualifier",
			stmt: stmt(
				INSERT, INTO, Users, sb.P(sb.I("name"), sb.I("email")),
				VALUES,
				sb.P("bob", "bob@x"),
				sb.P("amy", "amy@x"),
				ON, CONFLICT, sb.P(sb.I("email")),
				DO, UPDATE, SET, UsersName, "=", sb.I("excluded.name"),
				RETURNING, UsersID,
			),
			want: "INSERT INTO users (name, email)" +
				" VALUES" +
				" ($1, $2)," +
				" ($3, $4)" +
				" ON CONFLICT (email)" +
				" DO UPDATE SET name = excluded.name" +
				" RETURNING users.id",
			args: []any{"bob", "bob@x", "amy", "amy@x"},
		},
		{
			name: "UPDATE ... FROM, where FROM closes the SET list",
			stmt: stmt(
				UPDATE, Users, SET, UsersStatus, "=", "vip",
				FROM, Orders,
				WHERE, OrdersUserID, "=", UsersID, AND, OrdersTotal, ">", 10000,
			),
			want: "UPDATE users SET status = $1" +
				" FROM orders" +
				" WHERE orders.user_id = users.id AND orders.total > $2",
			args: []any{"vip", 10000},
		},
		{
			name: "OVER with a partition, a sort key and a frame clause",
			stmt: stmt(
				SELECT,
				UsersName,
				sb.F("SUM", OrdersTotal), OVER, sb.P(
					PARTITION, BY, OrdersUserID,
					ORDER, BY, OrdersCreated,
					ROWS, BETWEEN, UNBOUNDED, PRECEDING, AND, CURRENT, ROW,
				), AS, sb.I("running_total"),
				FROM, Orders,
			),
			want: "SELECT" +
				" users.name," +
				" SUM(orders.total) OVER" +
				" (PARTITION BY orders.user_id" +
				" ORDER BY orders.created_at" +
				" ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)" +
				" AS running_total" +
				" FROM orders",
		},
		{
			name: "JOIN LATERAL is a keyword, a group and an alias in sequence",
			stmt: stmt(
				SELECT, UsersName, sb.I("t.total"),
				FROM, Users,
				JOIN, LATERAL,
				sb.P(
					SELECT, OrdersID, OrdersTotal,
					FROM, Orders,
					WHERE, OrdersUserID, "=", UsersID,
					ORDER, BY, OrdersTotal, DESC,
					LIMIT, 3,
				),
				AS, sb.I("t"), ON, TRUE,
			),
			want: "SELECT users.name, t.total" +
				" FROM users" +
				" JOIN LATERAL" +
				" (SELECT orders.id, orders.total" +
				" FROM orders" +
				" WHERE orders.user_id = users.id" +
				" ORDER BY orders.total DESC" +
				" LIMIT $1)" +
				" AS t ON TRUE",
			args: []any{3},
		},
		{
			name: "WITH RECURSIVE over a UNION ALL",
			stmt: stmt(
				WITH, RECURSIVE, sb.I("tree"), AS,
				sb.P(
					SELECT, UsersID, UsersParentID,
					FROM, Users,
					WHERE, UsersID, "=", 9,

					UNION, ALL,

					SELECT, UsersID, UsersParentID,
					FROM, Users,
					JOIN, sb.I("tree"), ON, UsersParentID, "=", sb.I("tree.id"),
				),
				SELECT, "*",
				FROM, sb.I("tree"),
			),
			want: "WITH RECURSIVE tree AS" +
				" (SELECT users.id, users.parent_id" +
				" FROM users" +
				" WHERE users.id = $1" +
				" UNION ALL" +
				" SELECT users.id, users.parent_id" +
				" FROM users" +
				" JOIN tree ON users.parent_id = tree.id)" +
				" SELECT *" +
				" FROM tree",
			args: []any{9},
		},
		{
			name: "$N numbering across three levels of nesting",
			stmt: stmt(
				SELECT, sb.F("COUNT", "*"),
				FROM, sb.P(
					SELECT, UsersID,
					FROM, Users,
					WHERE, UsersID, IN, sb.P(
						SELECT, OrdersUserID,
						FROM, Orders,
						WHERE, OrdersTotal, ">", 100,
					), AND, UsersAge, ">", 18,
				), AS, sb.I("x"),
				WHERE, sb.I("x.id"), "<", 1000,
			),
			want: "SELECT COUNT(*)" +
				" FROM (SELECT users.id" +
				" FROM users" +
				" WHERE users.id IN (SELECT orders.user_id" +
				" FROM orders" +
				" WHERE orders.total > $1)" +
				" AND users.age > $2)" +
				" AS x" +
				" WHERE x.id < $3",
			args: []any{100, 18, 1000},
		},
		{
			name: "IN takes a list or a subquery, and ANY takes a subquery",
			stmt: stmt(
				SELECT, UsersID,
				FROM, Users,
				WHERE,
				UsersStatus, IN, sb.P("active", "trial"), AND,
				NOT, EXISTS, sb.P(SELECT, 1, FROM, Orders, WHERE, OrdersUserID, "=", UsersID), AND,
				UsersID, "=", ANY, sb.P(SELECT, OrdersUserID, FROM, Orders),
			),
			want: "SELECT users.id" +
				" FROM users" +
				" WHERE" +
				" users.status IN ($1, $2) AND" +
				" NOT EXISTS (SELECT $3 FROM orders WHERE orders.user_id = users.id) AND" +
				" users.id = ANY (SELECT orders.user_id FROM orders)",
			args: []any{"active", "trial", 1},
		},
	})
}
