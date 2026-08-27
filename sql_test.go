package psqlb

import (
	"github.com/fujidaiti/psqlb/sb"

	"strings"
	"testing"
)

// ===========================================================================
// Examples — run `go test` to check them
// ===========================================================================
//
// These are the usage documentation as well as the tests. Each one states the
// SQL it must produce and the arguments it must bind. The expected SQL is
// broken into lines that match the lines of Go above it, so the input and the
// output can be compared line by line.

const (
	Users          = sb.Id("users")
	UsersID        = sb.Id("users.id")
	UsersName      = sb.Id("users.name")
	UsersEmail     = sb.Id("users.email")
	UsersStatus    = sb.Id("users.status")
	UsersAge       = sb.Id("users.age")
	UsersCreated   = sb.Id("users.created_at")
	UsersMeta      = sb.Id("users.meta")
	UsersIsPaid    = sb.Id("users.paid")
	UsersHasTicket = sb.Id("users.has_ticket")
	UsersParentID  = sb.Id("users.parent_id")

	Orders        = sb.Id("orders")
	OrdersID      = sb.Id("orders.id")
	OrdersUserID  = sb.Id("orders.user_id")
	OrdersTotal   = sb.Id("orders.total")
	OrdersCreated = sb.Id("orders.created_at")
)

// assertSQL builds s and compares both halves of the result. Every argument
// bound by these examples is a comparable scalar, so == is enough, and it
// avoids the empty-versus-nil slice question.
func assertSQL(t *testing.T, s sb.Statement, wantSQL string, wantArgs ...any) {
	t.Helper()
	sql, args, err := s.ToSQL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sql != wantSQL {
		t.Errorf("sql:\n got: %s\nwant: %s", sql, wantSQL)
	}
	if len(args) != len(wantArgs) {
		t.Errorf("args:\n got: %#v\nwant: %#v", args, wantArgs)
		return
	}
	for i := range args {
		if args[i] != wantArgs[i] {
			t.Errorf("args[%d]:\n got: %#v\nwant: %#v", i, args[i], wantArgs[i])
		}
	}
}

// assertErr builds s and requires an error mentioning wantSubstr. Nothing in
// this package panics, so a panic here fails the test the same way.
func assertErr(t *testing.T, s sb.Statement, wantSubstr string) {
	t.Helper()
	sql, _, err := s.ToSQL()
	if err == nil {
		t.Fatalf("want error containing %q, got no error and sql %q", wantSubstr, sql)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error:\n got: %v\nwant substring: %s", err, wantSubstr)
	}
}

func TestBasic(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			SELECT, UsersID, UsersName,
			FROM, Users,
			WHERE,
			UsersID, EQ, sb.Lit(0), AND,
			UsersName, GT, sb.Lit("name"), AND,
			sb.Stm(UsersIsPaid, OR, UsersHasTicket),
		),
		"SELECT users.id, users.name"+
			" FROM users"+
			" WHERE"+
			" users.id = $1 AND"+
			" users.name > $2 AND"+
			" (users.paid OR users.has_ticket)",
		0, "name",
	)
}

// An item of a comma-separated clause spanning several tokens needs no wrapper.
// ORDER BY takes the whole of "users.created_at DESC" as one item because DESC
// is a postfix, so the comma falls before users.id rather than before DESC.
func TestKeyset(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			SELECT, UsersID,
			FROM, Users,
			WHERE, sb.Row(UsersCreated, UsersID), LT, sb.Row(sb.Lit("2025-06-01"), sb.Lit(500)),
			ORDER_BY, UsersCreated, DESC, UsersID, DESC, NULLS_LAST,
			LIMIT, sb.Lit(20),
		),
		"SELECT users.id"+
			" FROM users"+
			" WHERE (users.created_at, users.id) < ($1, $2)"+
			" ORDER BY users.created_at DESC, users.id DESC NULLS LAST"+
			" LIMIT $3",
		"2025-06-01", 500, 20,
	)
}

// Since nesting a sb.Stm adds parentheses, a set-operation term can carry LIMIT.
func TestUnionWithLimit(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			sb.Stm(SELECT, UsersID, FROM, Users, ORDER_BY, UsersID, LIMIT, sb.Lit(10)),
			UNION_ALL,
			sb.Stm(SELECT, OrdersUserID, FROM, Orders, ORDER_BY, OrdersID, LIMIT, sb.Lit(10)),
		),
		"(SELECT users.id FROM users ORDER BY users.id LIMIT $1)"+
			" UNION ALL"+
			" (SELECT orders.user_id FROM orders ORDER BY orders.id LIMIT $2)",
		10, 10,
	)
}

// Operators are written as keyword constants or via sb.Op(). There is no fixed
// list. sb.Raw covers an operand the package does not model.
func TestOperators(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			SELECT, UsersID,
			FROM, Users,
			WHERE,
			UsersMeta, sb.Op("@>"), sb.Raw(`'{"vip": true}'`), AND,
			UsersName, sb.Op("~"), sb.Lit("^a"), AND,
			UsersStatus, sb.Op("IS DISTINCT FROM"), sb.Lit("banned"), AND,
			UsersEmail, IS_NOT_NULL,
		),
		`SELECT users.id`+
			` FROM users`+
			` WHERE`+
			` users.meta @> '{"vip": true}' AND`+
			` users.name ~ $1 AND`+
			` users.status IS DISTINCT FROM $2 AND`+
			` users.email IS NOT NULL`,
		"^a", "banned",
	)
}

// sb.Raw takes values as well. Each "$0" in the fragment becomes the next
// placeholder, so an expression the package does not model keeps its values
// where they are written instead of being split into separate tokens.
func TestRawWithValues(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			SELECT, UsersID,
			FROM, Users,
			WHERE,
			sb.Raw("users.meta->'profile'->>'city' = $0", "Tokyo"), AND,
			sb.Raw("users.meta @> $0::jsonb", `{"vip": true}`), AND,
			UsersStatus, EQ, sb.Lit("active"),
		),
		"SELECT users.id"+
			" FROM users"+
			" WHERE"+
			" users.meta->'profile'->>'city' = $1 AND"+
			" users.meta @> $2::jsonb AND"+
			" users.status = $3",
		"Tokyo", `{"vip": true}`, "active",
	)
}

// DISTINCT_ON is a prefix, so it stays attached to the first item of the SELECT
// list. FILTER is an infix, so the group after it belongs to the aggregate
// before it and the alias that follows still belongs to the same item.
func TestDistinctOnAndFilter(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			SELECT,
			DISTINCT_ON(UsersID), UsersID,
			UsersName,
			sb.Func("COUNT", STAR), FILTER, sb.Stm(WHERE, UsersIsPaid), AS, sb.Id("paid_count"),
			FROM, Users,
			GROUP_BY, UsersID, UsersName,
			ORDER_BY, UsersID, UsersCreated, DESC,
		),
		"SELECT"+
			" DISTINCT ON (users.id) users.id,"+
			" users.name,"+
			" COUNT(*) FILTER (WHERE users.paid) AS paid_count"+
			" FROM users"+
			" GROUP BY users.id, users.name"+
			" ORDER BY users.id, users.created_at DESC",
	)
}

// A CASE expression sits in the middle of a SELECT list as plain tokens. None of
// CASE, WHEN, THEN, ELSE or END opens a list or takes a comma, so the commas of
// the SELECT list stay where they belong.
func TestCaseExpr(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			SELECT,
			UsersID,
			CASE,
			WHEN, UsersAge, GTE, sb.Lit(18), THEN, sb.Lit("adult"),
			WHEN, UsersAge, GTE, sb.Lit(13), THEN, sb.Lit("teen"),
			ELSE, sb.Lit("child"),
			END, AS, sb.Id("bucket"),
			FROM, Users,
		),
		"SELECT"+
			" users.id,"+
			" CASE"+
			" WHEN users.age >= $1 THEN $2"+
			" WHEN users.age >= $3 THEN $4"+
			" ELSE $5"+
			" END AS bucket"+
			" FROM users",
		18, "adult", 13, "teen", "child",
	)
}

func TestUpsert(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			INSERT_INTO, Users, sb.Row(sb.Id("name"), sb.Id("email")),
			VALUES,
			sb.Row(sb.Lit("bob"), sb.Lit("bob@x")),
			sb.Row(sb.Lit("amy"), sb.Lit("amy@x")),
			ON_CONFLICT, sb.Row(sb.Id("email")),
			DO_UPDATE, SET, UsersName, EQ, EXCLUDED, UsersName,
			RETURNING, UsersID,
		),
		"INSERT INTO users (name, email)"+
			" VALUES"+
			" ($1, $2),"+
			" ($3, $4)"+
			" ON CONFLICT (email)"+
			" DO UPDATE SET name = EXCLUDED.name"+
			" RETURNING users.id",
		"bob", "bob@x", "amy", "amy@x",
	)
}

// SET strips the qualifier from the name that begins each assignment. FROM
// closes the SET list, so orders.user_id keeps its qualifier.
func TestUpdateFrom(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			UPDATE, Users, SET, UsersStatus, EQ, sb.Lit("vip"),
			FROM, Orders,
			WHERE, OrdersUserID, EQ, UsersID, AND, OrdersTotal, GT, sb.Lit(10000),
		),
		"UPDATE users SET status = $1"+
			" FROM orders"+
			" WHERE orders.user_id = users.id AND orders.total > $2",
		"vip", 10000,
	)
}

// The window specification is a token list of its own. PARTITION BY and ORDER BY
// open their own lists inside it, and sb.Kw closes the last one so the frame is not
// read as another sort key.
func TestWindow(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			SELECT,
			UsersName,
			sb.Func("SUM", OrdersTotal), OVER, sb.Stm(
				PARTITION_BY, OrdersUserID,
				ORDER_BY, OrdersCreated,
				sb.Kw("ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW"),
			), AS, sb.Id("running_total"),
			FROM, Orders,
		),
		"SELECT"+
			" users.name,"+
			" SUM(orders.total) OVER"+
			" (PARTITION BY orders.user_id"+
			" ORDER BY orders.created_at"+
			" ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)"+
			" AS running_total"+
			" FROM orders",
	)
}

// LATERAL needs no dedicated syntax. It is just a prefix (LATERAL), an
// enclosure (sb.Stm) and a suffix (AS) placed in sequence.
func TestLateral(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			SELECT, UsersName, sb.Id("t.total"),
			FROM, Users,
			JOIN, LATERAL,
			sb.Stm(
				SELECT, OrdersID, OrdersTotal,
				FROM, Orders,
				WHERE, OrdersUserID, EQ, UsersID,
				ORDER_BY, OrdersTotal, DESC,
				LIMIT, sb.Lit(3),
			),
			AS, sb.Id("t"), ON, TRUE,
		),
		"SELECT users.name, t.total"+
			" FROM users"+
			" JOIN LATERAL"+
			" (SELECT orders.id, orders.total"+
			" FROM orders"+
			" WHERE orders.user_id = users.id"+
			" ORDER BY orders.total DESC"+
			" LIMIT $1)"+
			" AS t ON TRUE",
		3,
	)
}

// The two branches of the union are one flat token sequence, so neither is
// parenthesised. The body is a nested sb.Stm, which is where the parentheses a CTE
// body always has come from.
func TestRecursiveCTE(t *testing.T) {
	assertSQL(t,
		sb.Stm(
			WITH_RECURSIVE, sb.Id("tree"), AS,
			sb.Stm(
				SELECT, UsersID, UsersParentID,
				FROM, Users,
				WHERE, UsersID, EQ, sb.Lit(9),

				UNION_ALL,

				SELECT, UsersID, UsersParentID,
				FROM, Users,
				JOIN, sb.Id("tree"), ON, UsersParentID, EQ, sb.Id("tree.id"),
			),
			SELECT, STAR,
			FROM, sb.Id("tree"),
		),
		"WITH RECURSIVE tree AS"+
			" (SELECT users.id, users.parent_id"+
			" FROM users"+
			" WHERE users.id = $1"+
			" UNION ALL"+
			" SELECT users.id, users.parent_id"+
			" FROM users"+
			" JOIN tree ON users.parent_id = tree.id)"+
			" SELECT *"+
			" FROM tree",
		9,
	)
}

// $N numbering across three levels of nesting. The subquery after IN is kept as
// a []sb.Clause and spread into a sb.Stm, which is where its parentheses come from.
// The one used in FROM is a sb.Statement and takes its parentheses the same way.
func TestNested(t *testing.T) {
	inner := []sb.Clause{
		SELECT, OrdersUserID,
		FROM, Orders,
		WHERE, OrdersTotal, GT, sb.Lit(100),
	}
	middle := sb.Stm(
		SELECT, UsersID,
		FROM, Users,
		WHERE, UsersID, IN, sb.Stm(inner...), AND, UsersAge, GT, sb.Lit(18),
	)
	assertSQL(t,
		sb.Stm(
			SELECT, sb.Func("COUNT", STAR),
			FROM, middle, AS, sb.Id("x"),
			WHERE, sb.Id("x.id"), LT, sb.Lit(1000),
		),
		"SELECT COUNT(*)"+
			" FROM (SELECT users.id"+
			" FROM users"+
			" WHERE users.id IN (SELECT orders.user_id"+
			" FROM orders"+
			" WHERE orders.total > $1)"+
			" AND users.age > $2)"+
			" AS x"+
			" WHERE x.id < $3",
		100, 18, 1000,
	)
}

// IN takes a sb.Row for a list and a sb.Stm for a subquery; the keyword is the same
// either way. ANY takes a subquery: "= ANY" does not accept a list.
func TestInExistsNot(t *testing.T) {
	sub := []sb.Clause{SELECT, sb.Lit(1), FROM, Orders, WHERE, OrdersUserID, EQ, UsersID}
	assertSQL(t,
		sb.Stm(
			SELECT, UsersID,
			FROM, Users,
			WHERE,
			UsersStatus, IN, sb.Row(sb.Lit("active"), sb.Lit("trial")), AND,
			NOT, EXISTS, sb.Stm(sub...), AND,
			UsersID, EQ, ANY, sb.Stm(SELECT, OrdersUserID, FROM, Orders),
		),
		"SELECT users.id"+
			" FROM users"+
			" WHERE"+
			" users.status IN ($1, $2) AND"+
			" NOT EXISTS (SELECT $3 FROM orders WHERE orders.user_id = users.id) AND"+
			" users.id = ANY (SELECT orders.user_id FROM orders)",
		"active", "trial", 1,
	)
}

// Adding a clause and adding a condition are both written with the same append,
// since a clause keyword is a token like any other. Conditions are spliced in as
// tokens rather than wrapped, so no parentheses appear around them. Wrap them in
// sb.Stm where a group is needed.
func TestDynamic(t *testing.T) {
	cases := []struct {
		name   string
		status string
		cursor int
		sql    string
		args   []any
	}{
		{
			name: "no conditions",
			sql: "SELECT users.id" +
				" FROM users" +
				" ORDER BY users.id" +
				" LIMIT $1",
			args: []any{20},
		},
		{
			name:   "two conditions",
			status: "active",
			cursor: 100,
			sql: "SELECT users.id" +
				" FROM users" +
				" WHERE users.status = $1 AND users.id > $2" +
				" ORDER BY users.id" +
				" LIMIT $3",
			args: []any{"active", 100, 20},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var conds []sb.Clause
			add := func(tokens ...sb.Clause) {
				if len(conds) > 0 {
					conds = append(conds, AND)
				}
				conds = append(conds, tokens...)
			}
			if c.status != "" {
				add(UsersStatus, EQ, sb.Lit(c.status))
			}
			if c.cursor > 0 {
				add(UsersID, GT, sb.Lit(c.cursor))
			}

			parts := []sb.Clause{SELECT, UsersID, FROM, Users}
			if len(conds) > 0 {
				parts = append(append(parts, WHERE), conds...)
			}
			parts = append(parts, ORDER_BY, UsersID, LIMIT, sb.Lit(20))

			assertSQL(t, sb.Stm(parts...), c.sql, c.args...)
		})
	}
}

// ===========================================================================
// sb.Raw — the edges of $0 substitution
// ===========================================================================

func TestRawTooFewValues(t *testing.T) {
	// Leaving the marker in place would produce "b = $0", which Postgres rejects
	// with "there is no parameter $0", so this is an error.
	assertErr(t,
		sb.Stm(WHERE, sb.Raw("a = $0 AND b = $0", 1)),
		"2 $0 marker(s) but 1 value(s)",
	)
}

func TestRawSurplusValues(t *testing.T) {
	// Binding the surplus would leave the statement with a parameter nothing
	// refers to, and Postgres rejects the count mismatch, so this is an error
	// too.
	assertErr(t,
		sb.Stm(WHERE, sb.Raw("a = $0", 1, 2)),
		"1 $0 marker(s) but 2 value(s)",
	)
}

func TestRawDollarWithoutDigits(t *testing.T) {
	// A "$" with no digits after it is ordinary text, so the delimiters of a
	// dollar-quoted string pass through untouched.
	assertSQL(t,
		sb.Stm(SELECT, sb.Raw("$$it is here$$"), sb.Raw("$tag$so is this$tag$"), FROM, Users),
		"SELECT $$it is here$$, $tag$so is this$tag$ FROM users",
	)
}

func TestRawErrorsOnOtherPlaceholders(t *testing.T) {
	// A fragment cannot know its own position, so any number other than 0 is a
	// mistake. It is the one case reported although the string would run,
	// because a query that runs and reads another clause's value is worse than
	// one that fails. The last case is the price of that rule: a literal $1
	// inside a dollar-quoted body cannot be written, because sb.Raw cannot tell it
	// from a placeholder.
	for _, fragment := range []string{
		"a = $1",
		"a = $0 AND b = $2",
		"a = $01",
		"a = $00",
		"$$SELECT $1$$",
	} {
		t.Run(fragment, func(t *testing.T) {
			assertErr(t, sb.Stm(WHERE, sb.Raw(fragment)), "only $0 marks a value")
		})
	}
}
