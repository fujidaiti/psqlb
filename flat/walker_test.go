package sql

import (
	"fmt"
	"testing"
)

// ===========================================================================
// The walker — mechanical coverage
// ===========================================================================
//
// The examples in sql_test.go show what the DSL is for. These tests pin down the
// rules it rests on: which token takes a comma, which separator is in force,
// where the parentheses go, how the placeholders are numbered, and what is
// reported as an error.

type tcase struct {
	name string
	stm  Statement
	sql  string
	args []any
}

func run(t *testing.T, cases []tcase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertSQL(t, c.stm, c.sql, c.args...)
		})
	}
}

// ---------------------------------------------------------------------------
// Kinds — six kinds at each of the three positions
// ---------------------------------------------------------------------------

// Every case below is the same shape: a token under test placed inside an open
// SELECT list, with a trailing operand z that reveals whether the token left the
// item open. The three positions are reached with nothing before the token
// (posHead), with an infix before it (posGlue), and with a completed operand
// before it (posNext).
//
// This is the core invariant of the package, so all eighteen combinations are
// spelled out rather than derived.
func TestKindsAtEveryPosition(t *testing.T) {
	z := Id("z")
	run(t, []tcase{
		// posHead: the token is the first item of the list.
		{"head/operand", Stm(SELECT, Id("t"), z), "SELECT t, z", nil},
		{"head/prefix", Stm(SELECT, NOT, z), "SELECT NOT z", nil},
		{"head/postfix", Stm(SELECT, DESC, z), "SELECT DESC, z", nil},
		{"head/infix", Stm(SELECT, AND, z), "SELECT AND z", nil},
		{"head/list", Stm(SELECT, ORDER_BY, z), "SELECT ORDER BY z", nil},
		{"head/clause", Stm(SELECT, WHERE, z), "SELECT WHERE z", nil},

		// posGlue: an infix before the token leaves the item open, so the token
		// itself never takes a comma.
		{"glue/operand", Stm(SELECT, Id("a"), AS, Id("t"), z), "SELECT a AS t, z", nil},
		{"glue/prefix", Stm(SELECT, Id("a"), AS, NOT, z), "SELECT a AS NOT z", nil},
		{"glue/postfix", Stm(SELECT, Id("a"), AS, DESC, z), "SELECT a AS DESC, z", nil},
		{"glue/infix", Stm(SELECT, Id("a"), AS, AND, z), "SELECT a AS AND z", nil},
		{"glue/list", Stm(SELECT, Id("a"), AS, ORDER_BY, z), "SELECT a AS ORDER BY z", nil},
		{"glue/clause", Stm(SELECT, Id("a"), AS, WHERE, z), "SELECT a AS WHERE z", nil},

		// posNext: a completed operand before the token, so an operand or a
		// prefix takes the comma and nothing else does.
		{"next/operand", Stm(SELECT, Id("a"), Id("t"), z), "SELECT a, t, z", nil},
		{"next/prefix", Stm(SELECT, Id("a"), NOT, z), "SELECT a, NOT z", nil},
		{"next/postfix", Stm(SELECT, Id("a"), DESC, z), "SELECT a DESC, z", nil},
		{"next/infix", Stm(SELECT, Id("a"), AND, z), "SELECT a AND z", nil},
		{"next/list", Stm(SELECT, Id("a"), ORDER_BY, z), "SELECT a ORDER BY z", nil},
		{"next/clause", Stm(SELECT, Id("a"), WHERE, z), "SELECT a WHERE z", nil},
	})
}

// In space mode no token ever takes a comma, whatever its kind.
func TestNoCommasInSpaceMode(t *testing.T) {
	run(t, []tcase{
		{"operands", Stm(WHERE, Id("a"), Id("b"), Id("c")), "WHERE a b c", nil},
		{"mixed", Stm(WHERE, Id("a"), DESC, Id("b"), NOT, Id("c")), "WHERE a DESC b NOT c", nil},
		{"bare", Stm(Id("a"), Id("b")), "a b", nil},
	})
}

// ---------------------------------------------------------------------------
// Comma placement in real constructs
// ---------------------------------------------------------------------------

func TestCommaPlacement(t *testing.T) {
	cteA := Stm(SELECT, Lit(1))
	cteB := Stm(SELECT, Lit(2))

	run(t, []tcase{
		{
			"case nested in case",
			Stm(SELECT, UsersID,
				CASE, WHEN, UsersIsPaid, THEN,
				CASE, WHEN, UsersHasTicket, THEN, Lit(1), ELSE, Lit(2), END,
				ELSE, Lit(3), END,
				UsersName,
			),
			"SELECT users.id," +
				" CASE WHEN users.paid THEN" +
				" CASE WHEN users.has_ticket THEN $1 ELSE $2 END" +
				" ELSE $3 END," +
				" users.name",
			[]any{1, 2, 3},
		},
		{
			// A simple CASE, where the operand comes before the first WHEN.
			"simple case",
			Stm(SELECT, CASE, UsersStatus, WHEN, Lit("a"), THEN, Lit(1), END, UsersName),
			"SELECT CASE users.status WHEN $1 THEN $2 END, users.name",
			[]any{"a", 1},
		},
		{
			"distinct on two columns",
			Stm(SELECT, DISTINCT_ON(UsersID, UsersName), UsersID, UsersName, FROM, Users),
			"SELECT DISTINCT ON (users.id, users.name) users.id, users.name FROM users",
			nil,
		},
		{
			// IN is a postfix, so it does not take the comma and the item after
			// it does.
			"in mid-list",
			Stm(SELECT, Id("x"), IN(Lit(1)), Id("y"), FROM, Users),
			"SELECT x IN ($1), y FROM users",
			[]any{1},
		},
		{
			// EXISTS is an operand, so it does take the comma.
			"exists mid-list",
			Stm(SELECT, Id("x"), EXISTS(SELECT, Lit(1)), Id("y")),
			"SELECT x, EXISTS (SELECT $1), y",
			[]any{1},
		},
		{
			"not exists mid-list",
			Stm(SELECT, Id("x"), NOT, EXISTS(SELECT, Lit(1))),
			"SELECT x, NOT EXISTS (SELECT $1)",
			[]any{1},
		},
		{
			"not exists in space mode",
			Stm(WHERE, NOT, EXISTS(SELECT, Lit(1))),
			"WHERE NOT EXISTS (SELECT $1)",
			[]any{1},
		},
		{
			// MATERIALIZED is an infix, so it does not close the WITH list and
			// the comma before the second CTE survives.
			"two ctes one materialized",
			Stm(WITH, Id("a"), AS, MATERIALIZED, cteA, Id("b"), AS, cteB,
				SELECT, STAR, FROM, Id("a")),
			"WITH a AS MATERIALIZED (SELECT $1), b AS (SELECT $2) SELECT * FROM a",
			[]any{1, 2},
		},
		{
			"between inside a list",
			Stm(SELECT, UsersAge, Op("BETWEEN"), Lit(1), AND, Lit(2), UsersName),
			"SELECT users.age BETWEEN $1 AND $2, users.name",
			[]any{1, 2},
		},
		{
			"order by asc nulls first",
			Stm(ORDER_BY, UsersID, ASC, NULLS_FIRST, UsersName, DESC, NULLS_LAST),
			"ORDER BY users.id ASC NULLS FIRST, users.name DESC NULLS LAST",
			nil,
		},
		{
			"group by then having",
			Stm(SELECT, UsersID, FUNC("COUNT", STAR),
				FROM, Users,
				GROUP_BY, UsersID,
				HAVING, FUNC("COUNT", STAR), GT, Lit(1)),
			"SELECT users.id, COUNT(*) FROM users GROUP BY users.id HAVING COUNT(*) > $1",
			[]any{1},
		},
		{
			"from list then join",
			Stm(SELECT, STAR, FROM, Users, Orders, LEFT_JOIN, Id("p"), ON, TRUE),
			"SELECT * FROM users, orders LEFT JOIN p ON TRUE",
			nil,
		},
		{
			"is null postfix ends the item",
			Stm(SELECT, UsersEmail, IS_NOT_NULL, UsersName, IS_NULL),
			"SELECT users.email IS NOT NULL, users.name IS NULL",
			nil,
		},
		{
			"select distinct",
			Stm(SELECT, DISTINCT, UsersID, UsersName),
			"SELECT DISTINCT users.id, users.name",
			nil,
		},
		{
			"cast",
			Stm(SELECT, UsersMeta, CAST, Raw("jsonb"), UsersName),
			"SELECT users.meta :: jsonb, users.name",
			nil,
		},
	})
}

// ---------------------------------------------------------------------------
// Modes
// ---------------------------------------------------------------------------

func TestModes(t *testing.T) {
	run(t, []tcase{
		{
			"list reopened after a clause keyword closed it",
			Stm(SELECT, UsersID, UsersName, WHERE, TRUE, ORDER_BY, UsersID, UsersName),
			"SELECT users.id, users.name WHERE TRUE ORDER BY users.id, users.name",
			nil,
		},
		{
			"clause keyword closes the list mid-statement",
			Stm(SELECT, UsersID, LIMIT, Lit(1), OFFSET, Lit(2)),
			"SELECT users.id LIMIT $1 OFFSET $2",
			[]any{1, 2},
		},
		{
			"Kw closes the list like any clause keyword",
			Stm(SELECT, UsersID, Kw("FOR UPDATE"), Id("a"), Id("b")),
			"SELECT users.id FOR UPDATE a b",
			nil,
		},
		{
			"Row starts in list mode",
			Stm(Row(Id("a"), Id("b"))),
			"(a, b)",
			nil,
		},
		{
			"FUNC starts in list mode",
			Stm(FUNC("f", Id("a"), Id("b"))),
			"f(a, b)",
			nil,
		},
		{
			"Stm starts in space mode",
			Stm(Id("a"), Stm(Id("b"), Id("c"))),
			"a (b c)",
			nil,
		},
		{
			// A leading list keyword takes over the separator of a
			// paren-emitting function, which is what lets IN hold a subquery.
			"leading list keyword overrides list mode",
			Stm(WHERE, Id("x"), IN(SELECT, UsersID, UsersName, FROM, Users)),
			"WHERE x IN (SELECT users.id, users.name FROM users)",
			nil,
		},
		{
			"OVER holds a space-separated specification",
			Stm(SELECT, FUNC("SUM", Lit(1)), OVER(PARTITION_BY, UsersID, ORDER_BY, UsersCreated)),
			"SELECT SUM($1) OVER (PARTITION BY users.id ORDER BY users.created_at)",
			[]any{1},
		},
		{
			"OVER holds a window name",
			Stm(SELECT, FUNC("SUM", Lit(1)), OVER(Id("w"))),
			"SELECT SUM($1) OVER (w)",
			[]any{1},
		},
	})
}

// ---------------------------------------------------------------------------
// SET, INSERT INTO, ON CONFLICT and EXCLUDED — the bare-name positions
// ---------------------------------------------------------------------------

func TestBareNamePositions(t *testing.T) {
	run(t, []tcase{
		{
			"two assignments",
			Stm(UPDATE, Users, SET, UsersName, EQ, Lit("a"), UsersEmail, EQ, Lit("b")),
			"UPDATE users SET name = $1, email = $2",
			[]any{"a", "b"},
		},
		{
			// Only the name that begins the item is stripped, so a function call
			// on the right-hand side keeps its qualifier.
			"qualified right-hand side survives",
			Stm(UPDATE, Users, SET, UsersStatus, EQ, FUNC("upper", UsersName)),
			"UPDATE users SET status = upper(users.name)",
			nil,
		},
		{
			"qualified right-hand side as a bare Id survives",
			Stm(UPDATE, Users, SET, UsersStatus, EQ, UsersName),
			"UPDATE users SET status = users.name",
			nil,
		},
		{
			"FROM clears the set scope",
			Stm(UPDATE, Users, SET, UsersName, EQ, Lit("a"), FROM, Orders, Users),
			"UPDATE users SET name = $1 FROM orders, users",
			[]any{"a"},
		},
		{
			"RETURNING clears the set scope",
			Stm(UPDATE, Users, SET, UsersName, EQ, Lit("a"), RETURNING, UsersID, UsersName),
			"UPDATE users SET name = $1 RETURNING users.id, users.name",
			[]any{"a"},
		},
		{
			"WHERE clears the set scope",
			Stm(UPDATE, Users, SET, UsersName, EQ, Lit("a"), WHERE, UsersID, EQ, Lit(1)),
			"UPDATE users SET name = $1 WHERE users.id = $2",
			[]any{"a", 1},
		},
		{
			// EQ is an infix, not a mode-setting keyword, so it must not clear
			// the scope; otherwise the second assignment keeps its qualifier.
			"an infix does not clear the set scope",
			Stm(SET, UsersName, EQ, Lit("a"), UsersEmail, EQ, Lit("b")),
			"SET name = $1, email = $2",
			[]any{"a", "b"},
		},
		{
			"DELETE FROM",
			Stm(DELETE_FROM, Users, WHERE, UsersID, EQ, Lit(1)),
			"DELETE FROM users WHERE users.id = $1",
			[]any{1},
		},
		{
			"INSERT INTO with columns",
			Stm(INSERT_INTO(Users, UsersName, UsersEmail)),
			"INSERT INTO users (name, email)",
			nil,
		},
		{
			"INSERT INTO with no columns emits no parentheses",
			Stm(INSERT_INTO(Users), VALUES, Row(Lit(1))),
			"INSERT INTO users VALUES ($1)",
			[]any{1},
		},
		{
			"ON CONFLICT with one column",
			Stm(ON_CONFLICT(UsersEmail), DO_NOTHING),
			"ON CONFLICT (email) DO NOTHING",
			nil,
		},
		{
			"ON CONFLICT with several columns",
			Stm(ON_CONFLICT(UsersName, UsersEmail), DO_NOTHING),
			"ON CONFLICT (name, email) DO NOTHING",
			nil,
		},
		{
			"ON CONFLICT with no columns emits no parentheses",
			Stm(ON_CONFLICT(), DO_NOTHING),
			"ON CONFLICT DO NOTHING",
			nil,
		},
		{
			"two EXCLUDED assignments",
			Stm(SET, UsersName, EQ, EXCLUDED, UsersName, UsersEmail, EQ, EXCLUDED, UsersEmail),
			"SET name = EXCLUDED.name, email = EXCLUDED.email",
			nil,
		},
		{
			"EXCLUDED with an already bare name",
			Stm(SET, Id("name"), EQ, EXCLUDED, Id("name")),
			"SET name = EXCLUDED.name",
			nil,
		},
		{
			// EXCLUDED renders a complete operand, so a list item after it takes
			// the comma.
			"EXCLUDED ends the item",
			Stm(SET, UsersName, EQ, EXCLUDED, UsersName, UsersEmail, EQ, Lit(1)),
			"SET name = EXCLUDED.name, email = $1",
			[]any{1},
		},
	})
}

// ---------------------------------------------------------------------------
// Nesting and parentheses
// ---------------------------------------------------------------------------

func TestNestingAndParens(t *testing.T) {
	sub := Stm(SELECT, Lit(1))

	run(t, []tcase{
		{
			"three levels deep",
			Stm(Id("a"), Stm(Id("b"), Stm(Id("c"), Stm(Id("d"))))),
			"a (b (c (d)))",
			nil,
		},
		{
			"a Statement as a Row item",
			Stm(Row(sub, Lit(2))),
			"((SELECT $1), $2)",
			[]any{1, 2},
		},
		{
			"a Statement as a FUNC argument keeps both pairs",
			Stm(FUNC("coalesce", sub, Lit(0))),
			"coalesce((SELECT $1), $2)",
			[]any{1, 0},
		},
		{
			// Nesting adds parentheses here as everywhere, which makes this a
			// comparison against a scalar subquery rather than a membership test.
			"a Statement as the sole content of IN is double-wrapped",
			Stm(WHERE, Id("x"), IN(sub)),
			"WHERE x IN ((SELECT $1))",
			[]any{1},
		},
		{
			"the same tokens spread into IN are a membership test",
			Stm(WHERE, Id("x"), IN(SELECT, Lit(1))),
			"WHERE x IN (SELECT $1)",
			[]any{1},
		},
		{
			"Row inside Row",
			Stm(Row(Row(Id("a"), Id("b")), Id("c"))),
			"((a, b), c)",
			nil,
		},
		{
			"Row inside Stm inside Row",
			Stm(Row(Stm(Row(Id("a")), OR, Row(Id("b"))))),
			"(((a) OR (b)))",
			nil,
		},
		{
			"empty Row renders a pair",
			Stm(Row()),
			"()",
			nil,
		},
		{
			"two empty Rows in a list",
			Stm(SELECT, Row(), Row()),
			"SELECT (), ()",
			nil,
		},
		{
			"empty FUNC renders a pair",
			Stm(SELECT, FUNC("f")),
			"SELECT f()",
			nil,
		},
		{
			"an empty nested Stm renders nothing, not a pair",
			Stm(Id("a"), Stm(), Id("b")),
			"a b",
			nil,
		},
		{
			"an empty Stm at every level",
			Stm(Stm(Stm(Stm()))),
			"",
			nil,
		},
		{
			"the outermost level is never parenthesised",
			Stm(SELECT, Lit(1)),
			"SELECT $1",
			[]any{1},
		},
	})
}

// ---------------------------------------------------------------------------
// Multi-token arguments of a paren-emitting function
// ---------------------------------------------------------------------------

func TestMultiTokenArguments(t *testing.T) {
	run(t, []tcase{
		{
			"string_agg with ORDER BY",
			Stm(SELECT, FUNC("string_agg", UsersName, Lit(","), ORDER_BY, UsersID)),
			"SELECT string_agg(users.name, $1 ORDER BY users.id)",
			[]any{","},
		},
		{
			"COUNT DISTINCT",
			Stm(SELECT, FUNC("COUNT", DISTINCT, UsersID)),
			"SELECT COUNT(DISTINCT users.id)",
			nil,
		},
		{
			"EXTRACT",
			Stm(SELECT, FUNC("EXTRACT", Raw("EPOCH"), FROM, UsersCreated)),
			"SELECT EXTRACT(EPOCH FROM users.created_at)",
			nil,
		},
		{
			"FILTER holds a WHERE clause",
			Stm(SELECT, FUNC("COUNT", STAR), FILTER(WHERE, UsersIsPaid, AND, UsersHasTicket)),
			"SELECT COUNT(*) FILTER (WHERE users.paid AND users.has_ticket)",
			nil,
		},
		{
			"nested FUNC",
			Stm(SELECT, FUNC("upper", FUNC("coalesce", UsersName, Lit("")))),
			"SELECT upper(coalesce(users.name, $1))",
			[]any{""},
		},
		{
			"ANY holds a subquery",
			Stm(WHERE, UsersID, EQ, ANY(SELECT, OrdersUserID, FROM, Orders)),
			"WHERE users.id = ANY (SELECT orders.user_id FROM orders)",
			nil,
		},
	})
}

// ---------------------------------------------------------------------------
// $N numbering
// ---------------------------------------------------------------------------

func TestPlaceholderNumbering(t *testing.T) {
	sub := Stm(SELECT, Lit(7))

	run(t, []tcase{
		{
			"sequential across three levels",
			Stm(Lit(1), Stm(Lit(2), Stm(Lit(3))), Lit(4)),
			"$1 ($2 ($3)) $4",
			[]any{1, 2, 3, 4},
		},
		{
			"Lit and Raw interleaved",
			Stm(Lit("a"), Raw("f($0)", "b"), Lit("c")),
			"$1 f($2) $3",
			[]any{"a", "b", "c"},
		},
		{
			"a Raw marker after several Lits",
			Stm(Lit(1), Lit(2), Raw("x = $0", 3)),
			"$1 $2 x = $3",
			[]any{1, 2, 3},
		},
		{
			"two markers in one fragment",
			Stm(Raw("a = $0 AND b = $0", 1, 2)),
			"a = $1 AND b = $2",
			[]any{1, 2},
		},
		{
			// The same value is a template, not a binding, so each use binds its
			// own placeholder.
			"the same Statement used twice",
			Stm(sub, UNION_ALL, sub),
			"(SELECT $1) UNION ALL (SELECT $2)",
			[]any{7, 7},
		},
		{
			"numbering follows expansion order, not nesting depth",
			Stm(WHERE, Lit(1), AND, Id("x"), IN(Lit(2), Lit(3)), AND, Lit(4)),
			"WHERE $1 AND x IN ($2, $3) AND $4",
			[]any{1, 2, 3, 4},
		},
	})
}

// ---------------------------------------------------------------------------
// Empty renders
// ---------------------------------------------------------------------------

// A token that renders nothing is skipped without advancing the walker, so no
// dangling comma and no double space can appear. That is what lets an optional
// token be left in place as nil.
func TestEmptyRenders(t *testing.T) {
	run(t, []tcase{
		{"nil first in a list", Stm(SELECT, nil, UsersID, UsersName), "SELECT users.id, users.name", nil},
		{"nil in the middle", Stm(SELECT, UsersID, nil, UsersName), "SELECT users.id, users.name", nil},
		{"nil last", Stm(SELECT, UsersID, UsersName, nil), "SELECT users.id, users.name", nil},
		{"nil as the only item", Stm(SELECT, nil, FROM, Users), "SELECT FROM users", nil},
		{"nil between two operators", Stm(WHERE, TRUE, AND, nil, FALSE), "WHERE TRUE AND FALSE", nil},
		{"several nils in a row", Stm(SELECT, UsersID, nil, nil, UsersName), "SELECT users.id, users.name", nil},
		{"nil before the first token", Stm(nil, SELECT, UsersID), "SELECT users.id", nil},
		{"every token nil", Stm(nil, nil), "", nil},
		{"no tokens at all", Stm(), "", nil},
		{"an empty Id", Stm(SELECT, Id(""), UsersID), "SELECT users.id", nil},
		{"an empty Kw does not change the mode", Stm(SELECT, UsersID, Kw(""), UsersName), "SELECT users.id, users.name", nil},
		{"an empty Op", Stm(SELECT, UsersID, Op(""), UsersName), "SELECT users.id, users.name", nil},
		{"an empty Raw", Stm(SELECT, UsersID, Raw(""), UsersName), "SELECT users.id, users.name", nil},
		{"a nil inside Row", Stm(Row(Id("a"), nil, Id("b"))), "(a, b)", nil},
		{"a nil inside FUNC", Stm(FUNC("f", nil, Id("a"))), "f(a)", nil},
		{"a nil inside IN", Stm(WHERE, Id("x"), IN(Lit(1), nil)), "WHERE x IN ($1)", []any{1}},
	})
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func TestExcludedErrors(t *testing.T) {
	assertErr(t,
		Stm(SET, UsersName, EQ, EXCLUDED),
		"EXCLUDED is the last token",
	)
	assertErr(t,
		Stm(SET, UsersName, EQ, EXCLUDED, Lit(1)),
		"EXCLUDED must be followed by an Id",
	)
	assertErr(t,
		Stm(SET, UsersName, EQ, EXCLUDED, nil),
		"EXCLUDED must be followed by an Id",
	)
}

// An error raised anywhere reaches ToSQL, whatever it is nested inside.
func TestErrorsPropagate(t *testing.T) {
	bad := func() Clause { return Raw("a = $1") }

	for name, s := range map[string]Statement{
		"in a nested Stm":     Stm(SELECT, Stm(bad())),
		"in a Row":            Stm(Row(bad())),
		"in a FUNC":           Stm(SELECT, FUNC("f", bad())),
		"in an IN":            Stm(WHERE, Id("x"), IN(bad())),
		"in an EXISTS":        Stm(WHERE, EXISTS(bad())),
		"in an OVER":          Stm(SELECT, FUNC("f"), OVER(bad())),
		"three levels down":   Stm(Stm(Stm(bad()))),
		"EXCLUDED in a nest":  Stm(SELECT, Stm(SET, UsersName, EQ, EXCLUDED)),
		"after a good clause": Stm(SELECT, Lit(1), FROM, Users, WHERE, bad()),
	} {
		t.Run(name, func(t *testing.T) {
			sql, _, err := s.ToSQL()
			if err == nil {
				t.Fatalf("want an error, got sql %q", sql)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Building a statement in pieces
// ---------------------------------------------------------------------------

// A token list is an ordinary slice, so a fragment is built with append and
// spliced in with a spread. That is the only composition mechanism the package
// has, and it is what keeps a reused fragment out of parentheses it does not
// want.
func TestSpreadFragments(t *testing.T) {
	ids := []Clause{Lit(1), Lit(2), Lit(3)}
	subquery := []Clause{SELECT, OrdersUserID, FROM, Orders, WHERE, OrdersTotal, GT, Lit(100)}

	run(t, []tcase{
		{
			"a value list spread into IN",
			Stm(SELECT, UsersID, FROM, Users, WHERE, UsersID, IN(ids...)),
			"SELECT users.id FROM users WHERE users.id IN ($1, $2, $3)",
			[]any{1, 2, 3},
		},
		{
			"a subquery spread into IN",
			Stm(WHERE, UsersID, IN(subquery...)),
			"WHERE users.id IN (SELECT orders.user_id FROM orders WHERE orders.total > $1)",
			[]any{100},
		},
		{
			"a subquery spread into EXISTS",
			Stm(WHERE, EXISTS(subquery...)),
			"WHERE EXISTS (SELECT orders.user_id FROM orders WHERE orders.total > $1)",
			[]any{100},
		},
		{
			"the same tokens spread into Stm, which parenthesises them",
			Stm(FROM, Stm(subquery...), AS, Id("x")),
			"FROM (SELECT orders.user_id FROM orders WHERE orders.total > $1) AS x",
			[]any{100},
		},
		{
			"a conditions fragment spliced into a WHERE",
			Stm(append(
				append([]Clause{SELECT, UsersID, FROM, Users, WHERE},
					UsersStatus, EQ, Lit("active"), AND, UsersAge, GT, Lit(18)),
				ORDER_BY, UsersID)...),
			"SELECT users.id FROM users WHERE users.status = $1 AND users.age > $2 ORDER BY users.id",
			[]any{"active", 18},
		},
	})
}

// Stm copies its tokens, so mutating the slice afterwards does not change a
// statement already built from it.
func TestStmCopiesItsTokens(t *testing.T) {
	items := []Clause{SELECT, UsersID}
	s := Stm(items...)
	items[1] = UsersName
	assertSQL(t, s, "SELECT users.id")
}

// Row and Raw copy theirs too.
func TestRowAndRawCopyTheirInput(t *testing.T) {
	items := []Clause{Id("a"), Id("b")}
	r := Row(items...)
	items[0] = Id("z")
	assertSQL(t, Stm(r), "(a, b)")

	vals := []any{1}
	raw := Raw("x = $0", vals...)
	vals[0] = 99
	assertSQL(t, Stm(raw), "x = $1", 1)
}

// A statement is reusable: building it twice gives the same result, and the args
// of the first build are not carried into the second.
func TestBuildIsRepeatable(t *testing.T) {
	s := Stm(SELECT, UsersID, FROM, Users, WHERE, UsersID, EQ, Lit(1))
	for i := 0; i < 2; i++ {
		assertSQL(t, s, "SELECT users.id FROM users WHERE users.id = $1", 1)
	}
}

// ---------------------------------------------------------------------------
// A Clause implemented outside the package
// ---------------------------------------------------------------------------

// Clause is exported, so a caller can implement a token of their own. Such a
// token carries no kind, and the walker treats it as an operand: it begins an
// item of a comma-separated list and ends it.
type jsonPath string

func (p jsonPath) BuildSQL(args []any) (string, []any, error) {
	args = append(args, string(p))
	return fmt.Sprintf("users.meta #>> $%d", len(args)), args, nil
}

func TestCustomClauseIsAnOperand(t *testing.T) {
	run(t, []tcase{
		{
			"takes the comma of a list item",
			Stm(SELECT, UsersID, jsonPath("{a,b}"), UsersName),
			"SELECT users.id, users.meta #>> $1, users.name",
			[]any{"{a,b}"},
		},
		{
			"ends the item, so the next one takes a comma",
			Stm(SELECT, jsonPath("{a}"), AS, Id("v"), UsersName),
			"SELECT users.meta #>> $1 AS v, users.name",
			[]any{"{a}"},
		},
		{
			"and numbers its own placeholder in expansion order",
			Stm(WHERE, Lit(1), AND, jsonPath("{a}"), EQ, Lit(2)),
			"WHERE $1 AND users.meta #>> $2 = $3",
			[]any{1, "{a}", 2},
		},
	})
}
