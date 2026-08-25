package sql

import "testing"

// ===========================================================================
// Examples — run `go test` to check them
// ===========================================================================
//
// These are the usage documentation as well as the tests. Each one states the
// SQL it must produce and the arguments it must bind. The expected SQL is
// broken into lines that match the lines of Go above it, so the input and the
// output can be compared line by line.

const (
	Users          = Id("users")
	UsersID        = Id("users.id")
	UsersName      = Id("users.name")
	UsersEmail     = Id("users.email")
	UsersStatus    = Id("users.status")
	UsersAge       = Id("users.age")
	UsersCreated   = Id("users.created_at")
	UsersMeta      = Id("users.meta")
	UsersIsPaid    = Id("users.paid")
	UsersHasTicket = Id("users.has_ticket")
	UsersParentID  = Id("users.parent_id")

	Orders        = Id("orders")
	OrdersID      = Id("orders.id")
	OrdersUserID  = Id("orders.user_id")
	OrdersTotal   = Id("orders.total")
	OrdersCreated = Id("orders.created_at")
)

// assertSQL builds s and compares both halves of the result. Every argument
// bound by these examples is a comparable scalar, so == is enough, and it
// avoids the empty-versus-nil slice question.
func assertSQL(t *testing.T, s Statement, wantSQL string, wantArgs ...any) {
	t.Helper()
	sql, args := s.ToSQL()
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

func TestBasic(t *testing.T) {
	assertSQL(t,
		Stm(
			SELECT(UsersID, UsersName),
			FROM(Users),
			WHERE(
				UsersID, EQ, 0, AND,
				UsersName, GT, "name", AND,
				Stm(UsersIsPaid, OR, UsersHasTicket),
			),
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

// When one item of a comma-separated clause spans several tokens, wrap it in
// Stm. As an item of a comma-separated clause it carries no parentheses.
func TestKeyset(t *testing.T) {
	assertSQL(t,
		Stm(
			SELECT(UsersID),
			FROM(Users),
			WHERE(Row(UsersCreated, UsersID), LT, Row("2025-06-01", 500)),
			ORDER_BY(Stm(UsersCreated, DESC), Stm(UsersID, DESC, NULLS_LAST)),
			LIMIT(20),
		),
		"SELECT users.id"+
			" FROM users"+
			" WHERE (users.created_at, users.id) < ($1, $2)"+
			" ORDER BY users.created_at DESC, users.id DESC NULLS LAST"+
			" LIMIT $3",
		"2025-06-01", 500, 20,
	)
}

// Since nesting a Stm adds parentheses, a set-operation term can carry LIMIT.
func TestUnionWithLimit(t *testing.T) {
	assertSQL(t,
		Stm(
			Stm(SELECT(UsersID), FROM(Users), ORDER_BY(UsersID), LIMIT(10)),
			UNION_ALL,
			Stm(SELECT(OrdersUserID), FROM(Orders), ORDER_BY(OrdersID), LIMIT(10)),
		),
		"(SELECT users.id FROM users ORDER BY users.id LIMIT $1)"+
			" UNION ALL"+
			" (SELECT orders.user_id FROM orders ORDER BY orders.id LIMIT $2)",
		10, 10,
	)
}

// Operators are written as Raw constants or via Raw(). There is no fixed list.
func TestOperators(t *testing.T) {
	assertSQL(t,
		Stm(
			SELECT(UsersID),
			FROM(Users),
			WHERE(
				UsersMeta, Raw("@>"), Raw(`'{"vip": true}'`), AND,
				UsersName, Raw("~"), "^a", AND,
				UsersStatus, Raw("IS DISTINCT FROM"), "banned", AND,
				UsersEmail, IS_NOT_NULL,
			),
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

func TestDistinctOnAndFilter(t *testing.T) {
	assertSQL(t,
		Stm(
			SELECT(
				Stm(DISTINCT_ON(UsersID), UsersID),
				UsersName,
				Stm(FUNC("COUNT", STAR), FILTER, Stm(WHERE(UsersIsPaid)), AS("paid_count")),
			),
			FROM(Users),
			GROUP_BY(UsersID, UsersName),
			ORDER_BY(UsersID, Stm(UsersCreated, DESC)),
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

func TestCaseExpr(t *testing.T) {
	assertSQL(t,
		Stm(
			SELECT(
				UsersID,
				Stm(CASE,
					WHEN(UsersAge, GTE, 18), THEN("adult"),
					WHEN(UsersAge, GTE, 13), THEN("teen"),
					ELSE("child"),
					END, AS("bucket"),
				),
			),
			FROM(Users),
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
		Stm(
			INSERT_INTO(Users, UsersName, UsersEmail),
			VALUES(
				Row("bob", "bob@x"),
				Row("amy", "amy@x"),
			),
			ON_CONFLICT(UsersEmail),
			DO_UPDATE, SET(UsersName), EQ, EXCLUDED(UsersName),
			RETURNING(UsersID),
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

func TestUpdateFrom(t *testing.T) {
	assertSQL(t,
		Stm(
			UPDATE(Users), SET(UsersStatus), EQ, "vip",
			FROM(Orders),
			WHERE(OrdersUserID, EQ, UsersID, AND, OrdersTotal, GT, 10000),
		),
		"UPDATE users SET status = $1"+
			" FROM orders"+
			" WHERE orders.user_id = users.id AND orders.total > $2",
		"vip", 10000,
	)
}

func TestWindow(t *testing.T) {
	assertSQL(t,
		Stm(
			SELECT(
				UsersName,
				Stm(
					FUNC("SUM", OrdersTotal), OVER, Stm(
						PARTITION_BY(OrdersUserID),
						ORDER_BY(OrdersCreated),
						Raw("ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW"),
					),
					AS("running_total"),
				),
			),
			FROM(Orders),
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
// enclosure (Stm) and a suffix (AS) placed in sequence.
func TestLateral(t *testing.T) {
	perUser := Stm(
		SELECT(OrdersID, OrdersTotal),
		FROM(Orders),
		WHERE(OrdersUserID, EQ, UsersID),
		ORDER_BY(Stm(OrdersTotal, DESC)),
		LIMIT(3),
	)
	assertSQL(t,
		Stm(
			SELECT(UsersName, Id("t.total")),
			FROM(Users),
			JOIN, LATERAL, perUser, AS("t"), ON(TRUE),
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
// parenthesised. DEF supplies the parentheses the CTE body always has.
func TestRecursiveCTE(t *testing.T) {
	body := Stm(
		SELECT(UsersID, UsersParentID),
		FROM(Users),
		WHERE(UsersID, EQ, 1),

		UNION_ALL,

		SELECT(UsersID, UsersParentID),
		FROM(Users),
		JOIN, Id("tree"), ON(UsersParentID, EQ, Id("tree.id")),
	)
	assertSQL(t,
		Stm(
			WITH_RECURSIVE(DEF("tree", body)),
			SELECT(STAR),
			FROM(Id("tree")),
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
		1,
	)
}

// $N numbering across three levels of nesting.
func TestNested(t *testing.T) {
	inner := Stm(
		SELECT(OrdersUserID),
		FROM(Orders),
		WHERE(OrdersTotal, GT, 100),
	)
	middle := Stm(
		SELECT(UsersID),
		FROM(Users),
		WHERE(UsersID, IN(inner), AND, UsersAge, GT, 18),
	)
	assertSQL(t,
		Stm(
			SELECT(FUNC("COUNT", STAR)),
			FROM(Stm(middle, AS("x"))),
			WHERE(Id("x.id"), LT, 1000),
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

func TestInExistsNot(t *testing.T) {
	sub := Stm(SELECT(1), FROM(Orders), WHERE(OrdersUserID, EQ, UsersID))
	assertSQL(t,
		Stm(
			SELECT(UsersID),
			FROM(Users),
			WHERE(
				UsersStatus, IN(Row("active", "trial")), AND,
				NOT, EXISTS(sub), AND,
				UsersID, EQ, ANY(Row(1, 2, 3)),
			),
		),
		"SELECT users.id"+
			" FROM users"+
			" WHERE"+
			" users.status IN ($1, $2) AND"+
			" NOT EXISTS (SELECT $3 FROM orders WHERE orders.user_id = users.id) AND"+
			" users.id = ANY ($4, $5, $6)",
		"active", "trial", 1, 1, 2, 3,
	)
}

type filter struct {
	Status string
	Cursor int
}

// Adding a clause and adding a condition are both written with the same append.
// Conditions are spliced in as tokens rather than wrapped, so no parentheses
// appear around them. Wrap them in Stm where a group is needed.
func dynamic(f filter) Statement {
	var conds []any
	add := func(tokens ...any) {
		if len(conds) > 0 {
			conds = append(conds, AND)
		}
		conds = append(conds, tokens...)
	}
	if f.Status != "" {
		add(UsersStatus, EQ, f.Status)
	}
	if f.Cursor > 0 {
		add(UsersID, GT, f.Cursor)
	}

	parts := []any{SELECT(UsersID), FROM(Users)}
	if len(conds) > 0 {
		parts = append(parts, WHERE(conds...))
	}
	parts = append(parts, ORDER_BY(UsersID), LIMIT(20))

	return Stm(parts...)
}

func TestDynamicNoConditions(t *testing.T) {
	assertSQL(t,
		dynamic(filter{}),
		"SELECT users.id"+
			" FROM users"+
			" ORDER BY users.id"+
			" LIMIT $1",
		20,
	)
}

func TestDynamicTwoConditions(t *testing.T) {
	assertSQL(t,
		dynamic(filter{Status: "active", Cursor: 100}),
		"SELECT users.id"+
			" FROM users"+
			" WHERE users.status = $1 AND users.id > $2"+
			" ORDER BY users.id"+
			" LIMIT $3",
		"active", 100, 20,
	)
}
