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
//
// The grammar is built one phase at a time, and an example for a construct
// whose phase has not landed is skipped rather than deleted: it is written in
// its final spelling, so it says what that phase must produce. See the package
// doc of sb for the phases.

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
func assertSQL(t *testing.T, s sb.Group, wantSQL string, wantArgs ...any) {
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
func assertErr(t *testing.T, s sb.Group, wantSubstr string) {
	t.Helper()
	sql, _, err := s.ToSQL()
	if err == nil {
		t.Fatalf("want error containing %q, got no error and sql %q", wantSubstr, sql)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error:\n got: %v\nwant substring: %s", err, wantSubstr)
	}
}

// phase skips an example whose construct the grammar has not reached yet. The
// example is still compiled, so its spelling stays honest.
func phase(t *testing.T, n int, construct string) {
	t.Helper()
	t.Skipf("%s lands in phase %d", construct, n)
}

func TestBasic(t *testing.T) {
	assertSQL(t,
		sb.S(
			SELECT, UsersID, UsersName,
			FROM, Users,
			WHERE,
			UsersID, EQ, sb.Lit(0), AND,
			UsersName, GT, sb.Lit("name"), AND,
			sb.S(UsersIsPaid, OR, UsersHasTicket),
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
		sb.S(
			SELECT, UsersID,
			FROM, Users,
			WHERE, sb.S(UsersCreated, UsersID), LT, sb.S(sb.Lit("2025-06-01"), sb.Lit(500)),
			ORDER, BY, UsersCreated, DESC, UsersID, DESC, NULLS, LAST,
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

// Since nesting a sb.S adds parentheses, a set-operation term can carry LIMIT.
func TestUnionWithLimit(t *testing.T) {
	phase(t, 3, "set operations")
	assertSQL(t,
		sb.S(
			sb.S(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID, LIMIT, sb.Lit(10)),
			UNION, ALL,
			sb.S(SELECT, OrdersUserID, FROM, Orders, ORDER, BY, OrdersID, LIMIT, sb.Lit(10)),
		),
		"(SELECT users.id FROM users ORDER BY users.id LIMIT $1)"+
			" UNION ALL"+
			" (SELECT orders.user_id FROM orders ORDER BY orders.id LIMIT $2)",
		10, 10,
	)
}

// Operators are written as constants or via sb.Op(). There is no fixed list.
// IS NOT NULL and IS DISTINCT FROM are ordinary keyword sequences: the parser
// knows the phrases, so neither needs a constant or an escape hatch of its own.
func TestOperators(t *testing.T) {
	assertSQL(t,
		sb.S(
			SELECT, UsersID,
			FROM, Users,
			WHERE,
			UsersMeta, sb.Op("@>"), sb.Raw(`'{"vip": true}'`), AND,
			UsersName, sb.Op("~"), sb.Lit("^a"), AND,
			UsersStatus, IS, DISTINCT, FROM, sb.Lit("banned"), AND,
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

// sb.Raw takes values as well. Each "$0" in the fragment becomes the next
// placeholder, so an expression the package does not model keeps its values
// where they are written instead of being split into separate tokens.
func TestRawWithValues(t *testing.T) {
	assertSQL(t,
		sb.S(
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

// A cast written as tokens rather than inside a fragment. The type name is an
// sb.Id, and the parser glues both halves to the "::" because that is how SQL
// writes it.
func TestTypecast(t *testing.T) {
	assertSQL(t,
		sb.S(
			SELECT, UsersID,
			FROM, Users,
			WHERE, UsersMeta, sb.Op("@>"), sb.Lit(`{"vip": true}`), TYPECAST, sb.Id("jsonb"),
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
		sb.S(
			SELECT,
			DISTINCT, ON, sb.S(UsersID), UsersID,
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
	phase(t, 4, "FILTER")
	assertSQL(t,
		sb.S(
			SELECT,
			UsersID,
			UsersName,
			sb.Func("COUNT", STAR), FILTER, sb.S(WHERE, UsersIsPaid), AS, sb.Id("paid_count"),
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
		sb.S(
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

// EXCLUDED.name is an ordinary qualified name, written as one sb.Id. SET
// strips the qualifier from the name to the left of the "=" and leaves the
// expression to its right alone, so the two do not collide.
func TestUpsert(t *testing.T) {
	phase(t, 2, "INSERT")
	assertSQL(t,
		sb.S(
			INSERT, INTO, Users, sb.S(sb.Id("name"), sb.Id("email")),
			VALUES,
			sb.S(sb.Lit("bob"), sb.Lit("bob@x")),
			sb.S(sb.Lit("amy"), sb.Lit("amy@x")),
			ON, CONFLICT, sb.S(sb.Id("email")),
			DO, UPDATE, SET, UsersName, EQ, sb.Id("excluded.name"),
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
	phase(t, 2, "UPDATE")
	assertSQL(t,
		sb.S(
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

// The window specification is a token list of its own, and the frame clause is
// spelled out word by word rather than folded into one hand-written fragment.
func TestWindow(t *testing.T) {
	phase(t, 4, "window functions")
	assertSQL(t,
		sb.S(
			SELECT,
			UsersName,
			sb.Func("SUM", OrdersTotal), OVER, sb.S(
				PARTITION, BY, OrdersUserID,
				ORDER, BY, OrdersCreated,
				ROWS, BETWEEN, UNBOUNDED, PRECEDING, AND, CURRENT, ROW,
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

// LATERAL needs no dedicated syntax. It is a keyword, an enclosure (sb.S) and
// an alias placed in sequence.
func TestLateral(t *testing.T) {
	phase(t, 3, "joins")
	assertSQL(t,
		sb.S(
			SELECT, UsersName, sb.Id("t.total"),
			FROM, Users,
			JOIN, LATERAL,
			sb.S(
				SELECT, OrdersID, OrdersTotal,
				FROM, Orders,
				WHERE, OrdersUserID, EQ, UsersID,
				ORDER, BY, OrdersTotal, DESC,
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

func TestRecursiveCTE(t *testing.T) {
	phase(t, 4, "WITH")
	assertSQL(t,
		sb.S(
			WITH, RECURSIVE, sb.Id("tree"), AS,
			sb.S(
				SELECT, UsersID, UsersParentID,
				FROM, Users,
				WHERE, UsersID, EQ, sb.Lit(9),

				UNION, ALL,

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
// a []sb.Token and spread into a sb.S, which is where its parentheses come
// from. The one used in FROM is a sb.Group and takes its parentheses the same
// way.
func TestNested(t *testing.T) {
	inner := []sb.Token{
		SELECT, OrdersUserID,
		FROM, Orders,
		WHERE, OrdersTotal, GT, sb.Lit(100),
	}
	middle := sb.S(
		SELECT, UsersID,
		FROM, Users,
		WHERE, UsersID, IN, sb.S(inner...), AND, UsersAge, GT, sb.Lit(18),
	)
	assertSQL(t,
		sb.S(
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

// IN takes a sb.S either way, holding a list or a subquery according to the
// keywords inside it. ANY takes a subquery: "= ANY" does not accept a list.
func TestInExistsNot(t *testing.T) {
	sub := []sb.Token{SELECT, sb.Lit(1), FROM, Orders, WHERE, OrdersUserID, EQ, UsersID}
	assertSQL(t,
		sb.S(
			SELECT, UsersID,
			FROM, Users,
			WHERE,
			UsersStatus, IN, sb.S(sb.Lit("active"), sb.Lit("trial")), AND,
			NOT, EXISTS, sb.S(sub...), AND,
			UsersID, EQ, ANY, sb.S(SELECT, OrdersUserID, FROM, Orders),
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
// sb.S where a group is needed.
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
			var conds []sb.Token
			add := func(tokens ...sb.Token) {
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

			parts := []sb.Token{SELECT, UsersID, FROM, Users}
			if len(conds) > 0 {
				parts = append(append(parts, WHERE), conds...)
			}
			parts = append(parts, ORDER, BY, UsersID, LIMIT, sb.Lit(20))

			assertSQL(t, sb.S(parts...), c.sql, c.args...)
		})
	}
}

// An optional token is written nil, which is dropped before parsing. Removing a
// token can make the sequence ungrammatical, and that is now reported rather
// than emitted.
func TestNilIsAbsent(t *testing.T) {
	assertSQL(t,
		sb.S(SELECT, UsersID, nil, FROM, Users, nil),
		"SELECT users.id FROM users",
	)
}

// ===========================================================================
// sb.Raw — the edges of $0 substitution
// ===========================================================================

func TestRawTooFewValues(t *testing.T) {
	// Leaving the marker in place would produce "b = $0", which Postgres rejects
	// with "there is no parameter $0", so this is an error.
	assertErr(t,
		sb.S(SELECT, STAR, FROM, Users, WHERE, sb.Raw("a = $0 AND b = $0", 1)),
		"2 $0 marker(s) but 1 value(s)",
	)
}

func TestRawSurplusValues(t *testing.T) {
	// Binding the surplus would leave the statement with a parameter nothing
	// refers to, and Postgres rejects the count mismatch, so this is an error
	// too.
	assertErr(t,
		sb.S(SELECT, STAR, FROM, Users, WHERE, sb.Raw("a = $0", 1, 2)),
		"1 $0 marker(s) but 2 value(s)",
	)
}

func TestRawDollarWithoutDigits(t *testing.T) {
	// A "$" with no digits after it is ordinary text, so the delimiters of a
	// dollar-quoted string pass through untouched. Two fragments in a row are
	// two items of the SELECT list: the parser does not look inside either one,
	// so it takes each as one complete expression.
	assertSQL(t,
		sb.S(SELECT, sb.Raw("$$it is here$$"), sb.Raw("$tag$so is this$tag$"), FROM, Users),
		"SELECT $$it is here$$, $tag$so is this$tag$ FROM users",
	)
}

func TestRawErrorsOnOtherPlaceholders(t *testing.T) {
	// A fragment cannot know its own position, so any number other than 0 is a
	// mistake. It is the one case reported although the string would run,
	// because a query that runs and reads another clause's value is worse than
	// one that fails. The last case is the price of that rule: a literal $1
	// inside a dollar-quoted body cannot be written, because sb.Raw cannot tell
	// it from a placeholder.
	for _, fragment := range []string{
		"a = $1",
		"a = $0 AND b = $2",
		"a = $01",
		"a = $00",
		"$$SELECT $1$$",
	} {
		t.Run(fragment, func(t *testing.T) {
			assertErr(t, sb.S(SELECT, STAR, FROM, Users, WHERE, sb.Raw(fragment)), "only $0 marks a value")
		})
	}
}
