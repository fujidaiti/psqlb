package sb_test

import (
	. "github.com/fujidaiti/psqlb/kw"
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
	Users          = sb.I("users")
	UsersID        = sb.I("users.id")
	UsersName      = sb.I("users.name")
	UsersEmail     = sb.I("users.email")
	UsersStatus    = sb.I("users.status")
	UsersAge       = sb.I("users.age")
	UsersCreated   = sb.I("users.created_at")
	UsersMeta      = sb.I("users.meta")
	UsersIsPaid    = sb.I("users.paid")
	UsersHasTicket = sb.I("users.has_ticket")
	UsersParentID  = sb.I("users.parent_id")

	Orders        = sb.I("orders")
	OrdersID      = sb.I("orders.id")
	OrdersUserID  = sb.I("orders.user_id")
	OrdersTotal   = sb.I("orders.total")
	OrdersCreated = sb.I("orders.created_at")
)

// stmt collects its arguments into a []any, so a test case can write stmt(...)
// instead of []any{...} at each call to assertSQL or assertErr.
func stmt(items ...any) []any { return items }

// assertSQL builds s and compares both halves of the result. Every argument
// bound by these examples is a comparable scalar, so == is enough, and it
// avoids the empty-versus-nil slice question.
func assertSQL(t *testing.T, stmt []any, wantSQL string, wantArgs ...any) {
	t.Helper()
	sql, args, err := sb.ToSQL(stmt...)
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
func assertErr(t *testing.T, stmt []any, wantSubstr string) {
	t.Helper()
	sql, _, err := sb.ToSQL(stmt...)
	if err == nil {
		t.Fatalf("want error containing %q, got no error and sql %q", wantSubstr, sql)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error:\n got: %v\nwant substring: %s", err, wantSubstr)
	}
}

func TestBasic(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT, UsersID, UsersName,
			FROM, Users,
			WHERE,
			UsersID, EQ, 0, AND,
			UsersName, GT, "name", AND,
			sb.P(UsersIsPaid, OR, UsersHasTicket),
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
// The sort-key production is "expression [ASC|DESC] [NULLS FIRST|LAST]", so it
// takes the whole of "users.created_at DESC" as one item and the comma falls
// before users.id rather than before DESC.
func TestKeyset(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT, UsersID,
			FROM, Users,
			WHERE, sb.P(UsersCreated, UsersID), LT, sb.P("2025-06-01", 500),
			ORDER, BY, UsersCreated, DESC, UsersID, DESC, NULLS, LAST,
			LIMIT, 20,
		),
		"SELECT users.id"+
			" FROM users"+
			" WHERE (users.created_at, users.id) < ($1, $2)"+
			" ORDER BY users.created_at DESC, users.id DESC NULLS LAST"+
			" LIMIT $3",
		"2025-06-01", 500, 20,
	)
}

// Since nesting a sb.P adds parentheses, a set-operation term can carry LIMIT.
func TestUnionWithLimit(t *testing.T) {
	assertSQL(t,
		stmt(
			sb.P(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID, LIMIT, 10),
			UNION, ALL,
			sb.P(SELECT, OrdersUserID, FROM, Orders, ORDER, BY, OrdersID, LIMIT, 10),
		),
		"(SELECT users.id FROM users ORDER BY users.id LIMIT $1)"+
			" UNION ALL"+
			" (SELECT orders.user_id FROM orders ORDER BY orders.id LIMIT $2)",
		10, 10,
	)
}

// Operators are written as constants or via sb.RawOp(). There is no fixed list.
// IS NOT NULL and IS DISTINCT FROM are ordinary keyword sequences: the parser
// knows the phrases, so neither needs a constant or an escape hatch of its own.
func TestOperators(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT, UsersID,
			FROM, Users,
			WHERE,
			UsersMeta, sb.RawOp("@>"), sb.RawExpr(`'{"vip": true}'`), AND,
			UsersName, sb.RawOp("~"), "^a", AND,
			UsersStatus, IS, DISTINCT, FROM, "banned", AND,
			UsersEmail, IS, NOT, NULL,
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

// sb.RawExpr takes values as well. Each "$0" in the fragment becomes the next
// placeholder, so an expression the package does not model keeps its values
// where they are written instead of being split into separate tokens.
func TestRawWithValues(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT, UsersID,
			FROM, Users,
			WHERE,
			sb.RawExpr("users.meta->'profile'->>'city' = $0", "Tokyo"), AND,
			sb.RawExpr("users.meta @> $0::jsonb", `{"vip": true}`), AND,
			UsersStatus, EQ, "active",
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

// A cast written as tokens rather than inside a fragment. The type name is an
// sb.I, and the parser glues both halves to the "::" because that is how SQL
// writes it.
func TestTypecast(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT, UsersID,
			FROM, Users,
			WHERE, UsersMeta, sb.RawOp("@>"), `{"vip": true}`, TYPECAST, sb.I("jsonb"),
		),
		"SELECT users.id"+
			" FROM users"+
			" WHERE users.meta @> $1::jsonb",
		`{"vip": true}`,
	)
}

// DISTINCT ON is three ordinary tokens: two keywords and a group. The
// select-list production knows the group belongs to DISTINCT ON, so no comma
// follows it and no keyword has to carry parentheses of its own.
func TestDistinctOn(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT,
			DISTINCT, ON, sb.P(UsersID), UsersID,
			UsersName,
			FROM, Users,
			ORDER, BY, UsersID, UsersCreated, DESC,
		),
		"SELECT"+
			" DISTINCT ON (users.id) users.id,"+
			" users.name"+
			" FROM users"+
			" ORDER BY users.id, users.created_at DESC",
	)
}

// FILTER attaches a condition to an aggregate, and the alias after it still
// belongs to the same select-list item.
func TestFilterAndGroupBy(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT,
			UsersID,
			UsersName,
			sb.F("COUNT", STAR), FILTER, sb.P(WHERE, UsersIsPaid), AS, sb.I("paid_count"),
			FROM, Users,
			GROUP, BY, UsersID, UsersName,
		),
		"SELECT"+
			" users.id,"+
			" users.name,"+
			" COUNT(*) FILTER (WHERE users.paid) AS paid_count"+
			" FROM users"+
			" GROUP BY users.id, users.name",
	)
}

// A CASE expression sits in the middle of a SELECT list as plain tokens. It is
// one expression that ends at END, so the commas of the SELECT list stay where
// they belong.
func TestCaseExpr(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT,
			UsersID,
			CASE,
			WHEN, UsersAge, GTE, 18, THEN, "adult",
			WHEN, UsersAge, GTE, 13, THEN, "teen",
			ELSE, "child",
			END, AS, sb.I("bucket"),
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

// EXCLUDED.name is an ordinary qualified name, written as one sb.I. SET
// strips the qualifier from the name to the left of the "=" and leaves the
// expression to its right alone, so the two do not collide.
func TestUpsert(t *testing.T) {
	assertSQL(t,
		stmt(
			INSERT, INTO, Users, sb.P(sb.I("name"), sb.I("email")),
			VALUES,
			sb.P("bob", "bob@x"),
			sb.P("amy", "amy@x"),
			ON, CONFLICT, sb.P(sb.I("email")),
			DO, UPDATE, SET, UsersName, EQ, sb.I("excluded.name"),
			RETURNING, UsersID,
		),
		"INSERT INTO users (name, email)"+
			" VALUES"+
			" ($1, $2),"+
			" ($3, $4)"+
			" ON CONFLICT (email)"+
			" DO UPDATE SET name = excluded.name"+
			" RETURNING users.id",
		"bob", "bob@x", "amy", "amy@x",
	)
}

// SET strips the qualifier from the name that begins each assignment. FROM
// closes the SET list, so orders.user_id keeps its qualifier.
func TestUpdateFrom(t *testing.T) {
	assertSQL(t,
		stmt(
			UPDATE, Users, SET, UsersStatus, EQ, "vip",
			FROM, Orders,
			WHERE, OrdersUserID, EQ, UsersID, AND, OrdersTotal, GT, 10000,
		),
		"UPDATE users SET status = $1"+
			" FROM orders"+
			" WHERE orders.user_id = users.id AND orders.total > $2",
		"vip", 10000,
	)
}

// The window specification is a token list of its own, and the frame clause is
// spelled out word by word rather than folded into one hand-written fragment.
func TestWindow(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT,
			UsersName,
			sb.F("SUM", OrdersTotal), OVER, sb.P(
				PARTITION, BY, OrdersUserID,
				ORDER, BY, OrdersCreated,
				ROWS, BETWEEN, UNBOUNDED, PRECEDING, AND, CURRENT, ROW,
			), AS, sb.I("running_total"),
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

// LATERAL needs no dedicated syntax. It is a keyword, an enclosure (sb.P) and
// an alias placed in sequence.
func TestLateral(t *testing.T) {
	assertSQL(t,
		stmt(
			SELECT, UsersName, sb.I("t.total"),
			FROM, Users,
			JOIN, LATERAL,
			sb.P(
				SELECT, OrdersID, OrdersTotal,
				FROM, Orders,
				WHERE, OrdersUserID, EQ, UsersID,
				ORDER, BY, OrdersTotal, DESC,
				LIMIT, 3,
			),
			AS, sb.I("t"), ON, TRUE,
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

func TestRecursiveCTE(t *testing.T) {
	assertSQL(t,
		stmt(
			WITH, RECURSIVE, sb.I("tree"), AS,
			sb.P(
				SELECT, UsersID, UsersParentID,
				FROM, Users,
				WHERE, UsersID, EQ, 9,

				UNION, ALL,

				SELECT, UsersID, UsersParentID,
				FROM, Users,
				JOIN, sb.I("tree"), ON, UsersParentID, EQ, sb.I("tree.id"),
			),
			SELECT, STAR,
			FROM, sb.I("tree"),
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
// a []any and spread into a sb.P, which is where its parentheses come
// from. The one used in FROM is a sb.Group and takes its parentheses the same
// way.
func TestNested(t *testing.T) {
	inner := []any{
		SELECT, OrdersUserID,
		FROM, Orders,
		WHERE, OrdersTotal, GT, 100,
	}
	middle := sb.P(
		SELECT, UsersID,
		FROM, Users,
		WHERE, UsersID, IN, sb.P(inner...), AND, UsersAge, GT, 18,
	)
	assertSQL(t,
		stmt(
			SELECT, sb.F("COUNT", STAR),
			FROM, middle, AS, sb.I("x"),
			WHERE, sb.I("x.id"), LT, 1000,
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

// IN takes a sb.P either way, holding a list or a subquery according to the
// keywords inside it. ANY takes a subquery: "= ANY" does not accept a list.
func TestInExistsNot(t *testing.T) {
	sub := []any{SELECT, 1, FROM, Orders, WHERE, OrdersUserID, EQ, UsersID}
	assertSQL(t,
		stmt(
			SELECT, UsersID,
			FROM, Users,
			WHERE,
			UsersStatus, IN, sb.P("active", "trial"), AND,
			NOT, EXISTS, sb.P(sub...), AND,
			UsersID, EQ, ANY, sb.P(SELECT, OrdersUserID, FROM, Orders),
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
// sb.P where a group is needed.
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
			var conds []any
			add := func(tokens ...any) {
				if len(conds) > 0 {
					conds = append(conds, AND)
				}
				conds = append(conds, tokens...)
			}
			if c.status != "" {
				add(UsersStatus, EQ, c.status)
			}
			if c.cursor > 0 {
				add(UsersID, GT, c.cursor)
			}

			parts := []any{SELECT, UsersID, FROM, Users}
			if len(conds) > 0 {
				parts = append(append(parts, WHERE), conds...)
			}
			parts = append(parts, ORDER, BY, UsersID, LIMIT, 20)

			assertSQL(t, parts, c.sql, c.args...)
		})
	}
}

// A value made only of operator characters would be taken for an operator by
// the lexical rule, so sb.Arg is the override. That is the whole reason it
// exists, since every other value is written as itself.
func TestArg(t *testing.T) {
	assertSQL(t,
		stmt(SELECT, UsersID, FROM, Users, WHERE, UsersName, LIKE, sb.Arg("%")),
		"SELECT users.id FROM users WHERE users.name LIKE $1",
		"%",
	)
	// Without it, "%" is an operator and lands where an operand belongs.
	assertErr(t,
		stmt(SELECT, UsersID, FROM, Users, WHERE, UsersName, LIKE, "%"),
		"expected an expression",
	)
}

// nil is a value like any other and is bound. It used to mean "this token is
// absent" and was dropped before parsing; it no longer is, because a bare Go
// value cannot be told from an absent token, so dropping it would change the
// shape of a statement according to the data in it.
func TestNilIsAValue(t *testing.T) {
	assertSQL(t,
		stmt(SELECT, UsersID, FROM, Users, WHERE, UsersStatus, EQ, nil),
		"SELECT users.id FROM users WHERE users.status = $1",
		nil,
	)
}

// ===========================================================================
// sb.RawExpr — the edges of $0 substitution
// ===========================================================================

func TestRawTooFewValues(t *testing.T) {
	// Leaving the marker in place would produce "b = $0", which Postgres rejects
	// with "there is no parameter $0", so this is an error.
	assertErr(t,
		stmt(SELECT, STAR, FROM, Users, WHERE, sb.RawExpr("a = $0 AND b = $0", 1)),
		"2 $0 marker(s) but 1 value(s)",
	)
}

func TestRawSurplusValues(t *testing.T) {
	// Binding the surplus would leave the statement with a parameter nothing
	// refers to, and Postgres rejects the count mismatch, so this is an error
	// too.
	assertErr(t,
		stmt(SELECT, STAR, FROM, Users, WHERE, sb.RawExpr("a = $0", 1, 2)),
		"1 $0 marker(s) but 2 value(s)",
	)
}

func TestRawDollarWithoutDigits(t *testing.T) {
	// A "$" with no digits after it is ordinary text, so the delimiters of a
	// dollar-quoted string pass through untouched. Two fragments in a row are
	// two items of the SELECT list: the parser does not look inside either one,
	// so it takes each as one complete expression.
	assertSQL(t,
		stmt(SELECT, sb.RawExpr("$$it is here$$"), sb.RawExpr("$tag$so is this$tag$"), FROM, Users),
		"SELECT $$it is here$$, $tag$so is this$tag$ FROM users",
	)
}

func TestRawErrorsOnOtherPlaceholders(t *testing.T) {
	// A fragment cannot know its own position, so any number other than 0 is a
	// mistake. It is the one case reported although the string would run,
	// because a query that runs and reads another clause's value is worse than
	// one that fails. The last case is the price of that rule: a literal $1
	// inside a dollar-quoted body cannot be written, because sb.RawExpr cannot tell
	// it from a placeholder.
	for _, fragment := range []string{
		"a = $1",
		"a = $0 AND b = $2",
		"a = $01",
		"a = $00",
		"$$SELECT $1$$",
	} {
		t.Run(fragment, func(t *testing.T) {
			assertErr(t, stmt(SELECT, STAR, FROM, Users, WHERE, sb.RawExpr(fragment)), "only $0 marks a value")
		})
	}
}
