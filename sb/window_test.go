package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

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

// A window specification and a frame clause are checked the same way as
// everything else.
func TestWindowErrors(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "OVER something that is neither a name nor a definition",
			stmt: stmt(SELECT, sb.F("COUNT", "*"), OVER, 1, FROM, Users),
			want: "expected a window name or a parenthesised window definition",
		},
		{
			name: "FILTER without WHERE",
			stmt: stmt(SELECT, sb.F("COUNT", "*"), FILTER, sb.P(UsersIsPaid), FROM, Users),
			want: "FILTER: expected keyword WHERE",
		},
		{
			name: "FILTER without a group",
			stmt: stmt(SELECT, sb.F("COUNT", "*"), FILTER, UsersIsPaid, FROM, Users),
			want: "a parenthesised WHERE clause after FILTER is required",
		},
		{
			name: "a frame bound with no direction",
			stmt: stmt(SELECT, sb.F("COUNT", "*"), OVER, sb.P(ROWS, 3), FROM, Users),
			want: "frame clause: expected keyword PRECEDING or FOLLOWING",
		},
		{
			name: "BETWEEN in a frame without AND",
			stmt: stmt(SELECT, sb.F("COUNT", "*"),
				OVER, sb.P(ROWS, BETWEEN, UNBOUNDED, PRECEDING, CURRENT, ROW), FROM, Users),
			want: "frame clause: expected keyword AND",
		},
		{
			name: "a WINDOW entry without AS",
			stmt: stmt(SELECT, UsersID, FROM, Users, WINDOW, sb.I("w"), sb.P()),
			want: "WINDOW: expected keyword AS",
		},
	})
}
