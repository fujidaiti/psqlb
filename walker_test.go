package psqlb

import (
	"github.com/fujidaiti/psqlb/sb"

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
	stm  sb.Statement
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
	z := sb.Id("z")
	run(t, []tcase{
		// posHead: the token is the first item of the list.
		{"head/operand", sb.Stm(SELECT, sb.Id("t"), z), "SELECT t, z", nil},
		{"head/prefix", sb.Stm(SELECT, NOT, z), "SELECT NOT z", nil},
		{"head/postfix", sb.Stm(SELECT, DESC, z), "SELECT DESC, z", nil},
		{"head/infix", sb.Stm(SELECT, AND, z), "SELECT AND z", nil},
		{"head/list", sb.Stm(SELECT, ORDER_BY, z), "SELECT ORDER BY z", nil},
		{"head/clause", sb.Stm(SELECT, WHERE, z), "SELECT WHERE z", nil},

		// posGlue: an infix before the token leaves the item open, so the token
		// itself never takes a comma.
		{"glue/operand", sb.Stm(SELECT, sb.Id("a"), AS, sb.Id("t"), z), "SELECT a AS t, z", nil},
		{"glue/prefix", sb.Stm(SELECT, sb.Id("a"), AS, NOT, z), "SELECT a AS NOT z", nil},
		{"glue/postfix", sb.Stm(SELECT, sb.Id("a"), AS, DESC, z), "SELECT a AS DESC, z", nil},
		{"glue/infix", sb.Stm(SELECT, sb.Id("a"), AS, AND, z), "SELECT a AS AND z", nil},
		{"glue/list", sb.Stm(SELECT, sb.Id("a"), AS, ORDER_BY, z), "SELECT a AS ORDER BY z", nil},
		{"glue/clause", sb.Stm(SELECT, sb.Id("a"), AS, WHERE, z), "SELECT a AS WHERE z", nil},

		// posNext: a completed operand before the token, so an operand or a
		// prefix takes the comma and nothing else does.
		{"next/operand", sb.Stm(SELECT, sb.Id("a"), sb.Id("t"), z), "SELECT a, t, z", nil},
		{"next/prefix", sb.Stm(SELECT, sb.Id("a"), NOT, z), "SELECT a, NOT z", nil},
		{"next/postfix", sb.Stm(SELECT, sb.Id("a"), DESC, z), "SELECT a DESC, z", nil},
		{"next/infix", sb.Stm(SELECT, sb.Id("a"), AND, z), "SELECT a AND z", nil},
		{"next/list", sb.Stm(SELECT, sb.Id("a"), ORDER_BY, z), "SELECT a ORDER BY z", nil},
		{"next/clause", sb.Stm(SELECT, sb.Id("a"), WHERE, z), "SELECT a WHERE z", nil},
	})
}

// In space mode no token ever takes a comma, whatever its kind.
func TestNoCommasInSpaceMode(t *testing.T) {
	run(t, []tcase{
		{"operands", sb.Stm(WHERE, sb.Id("a"), sb.Id("b"), sb.Id("c")), "WHERE a b c", nil},
		{"mixed", sb.Stm(WHERE, sb.Id("a"), DESC, sb.Id("b"), NOT, sb.Id("c")), "WHERE a DESC b NOT c", nil},
		{"bare", sb.Stm(sb.Id("a"), sb.Id("b")), "a b", nil},
	})
}

// ---------------------------------------------------------------------------
// Comma placement in real constructs
// ---------------------------------------------------------------------------

func TestCommaPlacement(t *testing.T) {
	cteA := sb.Stm(SELECT, sb.Lit(1))
	cteB := sb.Stm(SELECT, sb.Lit(2))

	run(t, []tcase{
		{
			"case nested in case",
			sb.Stm(SELECT, UsersID,
				CASE, WHEN, UsersIsPaid, THEN,
				CASE, WHEN, UsersHasTicket, THEN, sb.Lit(1), ELSE, sb.Lit(2), END,
				ELSE, sb.Lit(3), END,
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
			sb.Stm(SELECT, CASE, UsersStatus, WHEN, sb.Lit("a"), THEN, sb.Lit(1), END, UsersName),
			"SELECT CASE users.status WHEN $1 THEN $2 END, users.name",
			[]any{"a", 1},
		},
		{
			"distinct on two columns",
			sb.Stm(SELECT, DISTINCT_ON(UsersID, UsersName), UsersID, UsersName, FROM, Users),
			"SELECT DISTINCT ON (users.id, users.name) users.id, users.name FROM users",
			nil,
		},
		{
			// IN is an infix, so the sb.Row after it does not take the comma and
			// the item after the sb.Row does.
			"in mid-list",
			sb.Stm(SELECT, sb.Id("x"), IN, sb.Row(sb.Lit(1)), sb.Id("y"), FROM, Users),
			"SELECT x IN ($1), y FROM users",
			[]any{1},
		},
		{
			// EXISTS is a prefix, so it begins an item and does take the comma.
			"exists mid-list",
			sb.Stm(SELECT, sb.Id("x"), EXISTS, sb.Stm(SELECT, sb.Lit(1)), sb.Id("y")),
			"SELECT x, EXISTS (SELECT $1), y",
			[]any{1},
		},
		{
			"not exists mid-list",
			sb.Stm(SELECT, sb.Id("x"), NOT, EXISTS, sb.Stm(SELECT, sb.Lit(1))),
			"SELECT x, NOT EXISTS (SELECT $1)",
			[]any{1},
		},
		{
			"not exists in space mode",
			sb.Stm(WHERE, NOT, EXISTS, sb.Stm(SELECT, sb.Lit(1))),
			"WHERE NOT EXISTS (SELECT $1)",
			[]any{1},
		},
		{
			// MATERIALIZED is an infix, so it does not close the WITH list and
			// the comma before the second CTE survives.
			"two ctes one materialized",
			sb.Stm(WITH, sb.Id("a"), AS, MATERIALIZED, cteA, sb.Id("b"), AS, cteB,
				SELECT, STAR, FROM, sb.Id("a")),
			"WITH a AS MATERIALIZED (SELECT $1), b AS (SELECT $2) SELECT * FROM a",
			[]any{1, 2},
		},
		{
			"between inside a list",
			sb.Stm(SELECT, UsersAge, sb.Op("BETWEEN"), sb.Lit(1), AND, sb.Lit(2), UsersName),
			"SELECT users.age BETWEEN $1 AND $2, users.name",
			[]any{1, 2},
		},
		{
			"order by asc nulls first",
			sb.Stm(ORDER_BY, UsersID, ASC, NULLS_FIRST, UsersName, DESC, NULLS_LAST),
			"ORDER BY users.id ASC NULLS FIRST, users.name DESC NULLS LAST",
			nil,
		},
		{
			"group by then having",
			sb.Stm(SELECT, UsersID, sb.Func("COUNT", STAR),
				FROM, Users,
				GROUP_BY, UsersID,
				HAVING, sb.Func("COUNT", STAR), GT, sb.Lit(1)),
			"SELECT users.id, COUNT(*) FROM users GROUP BY users.id HAVING COUNT(*) > $1",
			[]any{1},
		},
		{
			"from list then join",
			sb.Stm(SELECT, STAR, FROM, Users, Orders, LEFT_JOIN, sb.Id("p"), ON, TRUE),
			"SELECT * FROM users, orders LEFT JOIN p ON TRUE",
			nil,
		},
		{
			"is null postfix ends the item",
			sb.Stm(SELECT, UsersEmail, IS_NOT_NULL, UsersName, IS_NULL),
			"SELECT users.email IS NOT NULL, users.name IS NULL",
			nil,
		},
		{
			"select distinct",
			sb.Stm(SELECT, DISTINCT, UsersID, UsersName),
			"SELECT DISTINCT users.id, users.name",
			nil,
		},
		{
			"cast",
			sb.Stm(SELECT, UsersMeta, CAST, sb.Raw("jsonb"), UsersName),
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
			sb.Stm(SELECT, UsersID, UsersName, WHERE, TRUE, ORDER_BY, UsersID, UsersName),
			"SELECT users.id, users.name WHERE TRUE ORDER BY users.id, users.name",
			nil,
		},
		{
			"clause keyword closes the list mid-statement",
			sb.Stm(SELECT, UsersID, LIMIT, sb.Lit(1), OFFSET, sb.Lit(2)),
			"SELECT users.id LIMIT $1 OFFSET $2",
			[]any{1, 2},
		},
		{
			"Kw closes the list like any clause keyword",
			sb.Stm(SELECT, UsersID, sb.Kw("FOR UPDATE"), sb.Id("a"), sb.Id("b")),
			"SELECT users.id FOR UPDATE a b",
			nil,
		},
		{
			"Row starts in list mode",
			sb.Stm(sb.Row(sb.Id("a"), sb.Id("b"))),
			"(a, b)",
			nil,
		},
		{
			"Func starts in list mode",
			sb.Stm(sb.Func("f", sb.Id("a"), sb.Id("b"))),
			"f(a, b)",
			nil,
		},
		{
			"Stm starts in space mode",
			sb.Stm(sb.Id("a"), sb.Stm(sb.Id("b"), sb.Id("c"))),
			"a (b c)",
			nil,
		},
		{
			// A leading list keyword takes over the separator, which is what
			// lets a sb.Row hold a subquery.
			"leading list keyword overrides list mode",
			sb.Stm(WHERE, sb.Id("x"), IN, sb.Row(SELECT, UsersID, UsersName, FROM, Users)),
			"WHERE x IN (SELECT users.id, users.name FROM users)",
			nil,
		},
		{
			"OVER holds a space-separated specification",
			sb.Stm(SELECT, sb.Func("SUM", sb.Lit(1)), OVER, sb.Stm(PARTITION_BY, UsersID, ORDER_BY, UsersCreated)),
			"SELECT SUM($1) OVER (PARTITION BY users.id ORDER BY users.created_at)",
			[]any{1},
		},
		{
			"OVER holds a window name, which needs no parentheses",
			sb.Stm(SELECT, sb.Func("SUM", sb.Lit(1)), OVER, sb.Id("w")),
			"SELECT SUM($1) OVER w",
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
			sb.Stm(UPDATE, Users, SET, UsersName, EQ, sb.Lit("a"), UsersEmail, EQ, sb.Lit("b")),
			"UPDATE users SET name = $1, email = $2",
			[]any{"a", "b"},
		},
		{
			// Only the name that begins the item is stripped, so a function call
			// on the right-hand side keeps its qualifier.
			"qualified right-hand side survives",
			sb.Stm(UPDATE, Users, SET, UsersStatus, EQ, sb.Func("upper", UsersName)),
			"UPDATE users SET status = upper(users.name)",
			nil,
		},
		{
			"qualified right-hand side as a bare Id survives",
			sb.Stm(UPDATE, Users, SET, UsersStatus, EQ, UsersName),
			"UPDATE users SET status = users.name",
			nil,
		},
		{
			"FROM clears the set scope",
			sb.Stm(UPDATE, Users, SET, UsersName, EQ, sb.Lit("a"), FROM, Orders, Users),
			"UPDATE users SET name = $1 FROM orders, users",
			[]any{"a"},
		},
		{
			"RETURNING clears the set scope",
			sb.Stm(UPDATE, Users, SET, UsersName, EQ, sb.Lit("a"), RETURNING, UsersID, UsersName),
			"UPDATE users SET name = $1 RETURNING users.id, users.name",
			[]any{"a"},
		},
		{
			"WHERE clears the set scope",
			sb.Stm(UPDATE, Users, SET, UsersName, EQ, sb.Lit("a"), WHERE, UsersID, EQ, sb.Lit(1)),
			"UPDATE users SET name = $1 WHERE users.id = $2",
			[]any{"a", 1},
		},
		{
			// EQ is an infix, not a mode-setting keyword, so it must not clear
			// the scope; otherwise the second assignment keeps its qualifier.
			"an infix does not clear the set scope",
			sb.Stm(SET, UsersName, EQ, sb.Lit("a"), UsersEmail, EQ, sb.Lit("b")),
			"SET name = $1, email = $2",
			[]any{"a", "b"},
		},
		{
			"DELETE FROM",
			sb.Stm(DELETE_FROM, Users, WHERE, UsersID, EQ, sb.Lit(1)),
			"DELETE FROM users WHERE users.id = $1",
			[]any{1},
		},
		{
			"INSERT INTO with columns",
			sb.Stm(INSERT_INTO, Users, sb.Row(sb.Id("name"), sb.Id("email"))),
			"INSERT INTO users (name, email)",
			nil,
		},
		{
			"INSERT INTO with no columns",
			sb.Stm(INSERT_INTO, Users, VALUES, sb.Row(sb.Lit(1))),
			"INSERT INTO users VALUES ($1)",
			[]any{1},
		},
		{
			"ON CONFLICT with one column",
			sb.Stm(ON_CONFLICT, sb.Row(sb.Id("email")), DO_NOTHING),
			"ON CONFLICT (email) DO NOTHING",
			nil,
		},
		{
			"ON CONFLICT with several columns",
			sb.Stm(ON_CONFLICT, sb.Row(sb.Id("name"), sb.Id("email")), DO_NOTHING),
			"ON CONFLICT (name, email) DO NOTHING",
			nil,
		},
		{
			"ON CONFLICT with no columns",
			sb.Stm(ON_CONFLICT, DO_NOTHING),
			"ON CONFLICT DO NOTHING",
			nil,
		},
		{
			"two EXCLUDED assignments",
			sb.Stm(SET, UsersName, EQ, EXCLUDED, UsersName, UsersEmail, EQ, EXCLUDED, UsersEmail),
			"SET name = EXCLUDED.name, email = EXCLUDED.email",
			nil,
		},
		{
			"EXCLUDED with an already bare name",
			sb.Stm(SET, sb.Id("name"), EQ, EXCLUDED, sb.Id("name")),
			"SET name = EXCLUDED.name",
			nil,
		},
		{
			// EXCLUDED renders a complete operand, so a list item after it takes
			// the comma.
			"EXCLUDED ends the item",
			sb.Stm(SET, UsersName, EQ, EXCLUDED, UsersName, UsersEmail, EQ, sb.Lit(1)),
			"SET name = EXCLUDED.name, email = $1",
			[]any{1},
		},
	})
}

// ---------------------------------------------------------------------------
// Nesting and parentheses
// ---------------------------------------------------------------------------

func TestNestingAndParens(t *testing.T) {
	sub := sb.Stm(SELECT, sb.Lit(1))

	run(t, []tcase{
		{
			"three levels deep",
			sb.Stm(sb.Id("a"), sb.Stm(sb.Id("b"), sb.Stm(sb.Id("c"), sb.Stm(sb.Id("d"))))),
			"a (b (c (d)))",
			nil,
		},
		{
			"a Statement as a Row item",
			sb.Stm(sb.Row(sub, sb.Lit(2))),
			"((SELECT $1), $2)",
			[]any{1, 2},
		},
		{
			"a Statement as a Func argument keeps both pairs",
			sb.Stm(sb.Func("coalesce", sub, sb.Lit(0))),
			"coalesce((SELECT $1), $2)",
			[]any{1, 0},
		},
		{
			// A sb.Row holding a nested sb.Statement is a one-element list containing
			// a scalar subquery, not a membership test.
			"a Statement inside a Row after IN is double-wrapped",
			sb.Stm(WHERE, sb.Id("x"), IN, sb.Row(sub)),
			"WHERE x IN ((SELECT $1))",
			[]any{1},
		},
		{
			"the same tokens in a Stm after IN are a membership test",
			sb.Stm(WHERE, sb.Id("x"), IN, sb.Stm(SELECT, sb.Lit(1))),
			"WHERE x IN (SELECT $1)",
			[]any{1},
		},
		{
			"Row inside Row",
			sb.Stm(sb.Row(sb.Row(sb.Id("a"), sb.Id("b")), sb.Id("c"))),
			"((a, b), c)",
			nil,
		},
		{
			"Row inside Stm inside Row",
			sb.Stm(sb.Row(sb.Stm(sb.Row(sb.Id("a")), OR, sb.Row(sb.Id("b"))))),
			"(((a) OR (b)))",
			nil,
		},
		{
			"empty Row renders a pair",
			sb.Stm(sb.Row()),
			"()",
			nil,
		},
		{
			"two empty Rows in a list",
			sb.Stm(SELECT, sb.Row(), sb.Row()),
			"SELECT (), ()",
			nil,
		},
		{
			"empty Func renders a pair",
			sb.Stm(SELECT, sb.Func("f")),
			"SELECT f()",
			nil,
		},
		{
			"an empty nested Stm renders nothing, not a pair",
			sb.Stm(sb.Id("a"), sb.Stm(), sb.Id("b")),
			"a b",
			nil,
		},
		{
			"an empty Stm at every level",
			sb.Stm(sb.Stm(sb.Stm(sb.Stm()))),
			"",
			nil,
		},
		{
			"the outermost level is never parenthesised",
			sb.Stm(SELECT, sb.Lit(1)),
			"SELECT $1",
			[]any{1},
		},
	})
}

// ---------------------------------------------------------------------------
// Multi-token arguments of a parenthesised group
// ---------------------------------------------------------------------------

func TestMultiTokenArguments(t *testing.T) {
	run(t, []tcase{
		{
			"string_agg with ORDER BY",
			sb.Stm(SELECT, sb.Func("string_agg", UsersName, sb.Lit(","), ORDER_BY, UsersID)),
			"SELECT string_agg(users.name, $1 ORDER BY users.id)",
			[]any{","},
		},
		{
			"COUNT DISTINCT",
			sb.Stm(SELECT, sb.Func("COUNT", DISTINCT, UsersID)),
			"SELECT COUNT(DISTINCT users.id)",
			nil,
		},
		{
			"EXTRACT",
			sb.Stm(SELECT, sb.Func("EXTRACT", sb.Raw("EPOCH"), FROM, UsersCreated)),
			"SELECT EXTRACT(EPOCH FROM users.created_at)",
			nil,
		},
		{
			"FILTER holds a WHERE clause",
			sb.Stm(SELECT, sb.Func("COUNT", STAR), FILTER, sb.Stm(WHERE, UsersIsPaid, AND, UsersHasTicket)),
			"SELECT COUNT(*) FILTER (WHERE users.paid AND users.has_ticket)",
			nil,
		},
		{
			"nested Func",
			sb.Stm(SELECT, sb.Func("upper", sb.Func("coalesce", UsersName, sb.Lit("")))),
			"SELECT upper(coalesce(users.name, $1))",
			[]any{""},
		},
		{
			"ANY holds a subquery",
			sb.Stm(WHERE, UsersID, EQ, ANY, sb.Stm(SELECT, OrdersUserID, FROM, Orders)),
			"WHERE users.id = ANY (SELECT orders.user_id FROM orders)",
			nil,
		},
	})
}

// ---------------------------------------------------------------------------
// $N numbering
// ---------------------------------------------------------------------------

func TestPlaceholderNumbering(t *testing.T) {
	sub := sb.Stm(SELECT, sb.Lit(7))

	run(t, []tcase{
		{
			"sequential across three levels",
			sb.Stm(sb.Lit(1), sb.Stm(sb.Lit(2), sb.Stm(sb.Lit(3))), sb.Lit(4)),
			"$1 ($2 ($3)) $4",
			[]any{1, 2, 3, 4},
		},
		{
			"Lit and Raw interleaved",
			sb.Stm(sb.Lit("a"), sb.Raw("f($0)", "b"), sb.Lit("c")),
			"$1 f($2) $3",
			[]any{"a", "b", "c"},
		},
		{
			"a Raw marker after several Lits",
			sb.Stm(sb.Lit(1), sb.Lit(2), sb.Raw("x = $0", 3)),
			"$1 $2 x = $3",
			[]any{1, 2, 3},
		},
		{
			"two markers in one fragment",
			sb.Stm(sb.Raw("a = $0 AND b = $0", 1, 2)),
			"a = $1 AND b = $2",
			[]any{1, 2},
		},
		{
			// The same value is a template, not a binding, so each use binds its
			// own placeholder.
			"the same Statement used twice",
			sb.Stm(sub, UNION_ALL, sub),
			"(SELECT $1) UNION ALL (SELECT $2)",
			[]any{7, 7},
		},
		{
			"numbering follows expansion order, not nesting depth",
			sb.Stm(WHERE, sb.Lit(1), AND, sb.Id("x"), IN, sb.Row(sb.Lit(2), sb.Lit(3)), AND, sb.Lit(4)),
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
		{"nil first in a list", sb.Stm(SELECT, nil, UsersID, UsersName), "SELECT users.id, users.name", nil},
		{"nil in the middle", sb.Stm(SELECT, UsersID, nil, UsersName), "SELECT users.id, users.name", nil},
		{"nil last", sb.Stm(SELECT, UsersID, UsersName, nil), "SELECT users.id, users.name", nil},
		{"nil as the only item", sb.Stm(SELECT, nil, FROM, Users), "SELECT FROM users", nil},
		{"nil between two operators", sb.Stm(WHERE, TRUE, AND, nil, FALSE), "WHERE TRUE AND FALSE", nil},
		{"several nils in a row", sb.Stm(SELECT, UsersID, nil, nil, UsersName), "SELECT users.id, users.name", nil},
		{"nil before the first token", sb.Stm(nil, SELECT, UsersID), "SELECT users.id", nil},
		{"every token nil", sb.Stm(nil, nil), "", nil},
		{"no tokens at all", sb.Stm(), "", nil},
		{"an empty Id", sb.Stm(SELECT, sb.Id(""), UsersID), "SELECT users.id", nil},
		{"an empty Kw does not change the mode", sb.Stm(SELECT, UsersID, sb.Kw(""), UsersName), "SELECT users.id, users.name", nil},
		{"an empty Op", sb.Stm(SELECT, UsersID, sb.Op(""), UsersName), "SELECT users.id, users.name", nil},
		{"an empty Raw", sb.Stm(SELECT, UsersID, sb.Raw(""), UsersName), "SELECT users.id, users.name", nil},
		{"a nil inside Row", sb.Stm(sb.Row(sb.Id("a"), nil, sb.Id("b"))), "(a, b)", nil},
		{"a nil inside Func", sb.Stm(sb.Func("f", nil, sb.Id("a"))), "f(a)", nil},
		{"a nil inside a Row after IN", sb.Stm(WHERE, sb.Id("x"), IN, sb.Row(sb.Lit(1), nil)), "WHERE x IN ($1)", []any{1}},
	})
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func TestExcludedErrors(t *testing.T) {
	assertErr(t,
		sb.Stm(SET, UsersName, EQ, EXCLUDED),
		"EXCLUDED is the last token",
	)
	assertErr(t,
		sb.Stm(SET, UsersName, EQ, EXCLUDED, sb.Lit(1)),
		"EXCLUDED must be followed by an Id",
	)
	assertErr(t,
		sb.Stm(SET, UsersName, EQ, EXCLUDED, nil),
		"EXCLUDED must be followed by an Id",
	)
}

// An error raised anywhere reaches ToSQL, whatever it is nested inside.
func TestErrorsPropagate(t *testing.T) {
	bad := func() sb.Clause { return sb.Raw("a = $1") }

	for name, s := range map[string]sb.Statement{
		"in a nested Stm":     sb.Stm(SELECT, sb.Stm(bad())),
		"in a Row":            sb.Stm(sb.Row(bad())),
		"in a Func":           sb.Stm(SELECT, sb.Func("f", bad())),
		"in a Row after IN":   sb.Stm(WHERE, sb.Id("x"), IN, sb.Row(bad())),
		"in a Stm after IN":   sb.Stm(WHERE, sb.Id("x"), IN, sb.Stm(bad())),
		"in a DISTINCT ON":    sb.Stm(SELECT, DISTINCT_ON(bad())),
		"three levels down":   sb.Stm(sb.Stm(sb.Stm(bad()))),
		"EXCLUDED in a nest":  sb.Stm(SELECT, sb.Stm(SET, UsersName, EQ, EXCLUDED)),
		"after a good clause": sb.Stm(SELECT, sb.Lit(1), FROM, Users, WHERE, bad()),
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
	ids := []sb.Clause{sb.Lit(1), sb.Lit(2), sb.Lit(3)}
	subquery := []sb.Clause{SELECT, OrdersUserID, FROM, Orders, WHERE, OrdersTotal, GT, sb.Lit(100)}

	run(t, []tcase{
		{
			"a value list spread into a Row after IN",
			sb.Stm(SELECT, UsersID, FROM, Users, WHERE, UsersID, IN, sb.Row(ids...)),
			"SELECT users.id FROM users WHERE users.id IN ($1, $2, $3)",
			[]any{1, 2, 3},
		},
		{
			"a subquery spread into a Stm after IN",
			sb.Stm(WHERE, UsersID, IN, sb.Stm(subquery...)),
			"WHERE users.id IN (SELECT orders.user_id FROM orders WHERE orders.total > $1)",
			[]any{100},
		},
		{
			"a subquery spread into a Stm after EXISTS",
			sb.Stm(WHERE, EXISTS, sb.Stm(subquery...)),
			"WHERE EXISTS (SELECT orders.user_id FROM orders WHERE orders.total > $1)",
			[]any{100},
		},
		{
			"the same tokens spread into Stm, which parenthesises them",
			sb.Stm(FROM, sb.Stm(subquery...), AS, sb.Id("x")),
			"FROM (SELECT orders.user_id FROM orders WHERE orders.total > $1) AS x",
			[]any{100},
		},
		{
			"a conditions fragment spliced into a WHERE",
			sb.Stm(append(
				append([]sb.Clause{SELECT, UsersID, FROM, Users, WHERE},
					UsersStatus, EQ, sb.Lit("active"), AND, UsersAge, GT, sb.Lit(18)),
				ORDER_BY, UsersID)...),
			"SELECT users.id FROM users WHERE users.status = $1 AND users.age > $2 ORDER BY users.id",
			[]any{"active", 18},
		},
	})
}

// sb.Stm copies its tokens, so mutating the slice afterwards does not change a
// statement already built from it.
func TestStmCopiesItsTokens(t *testing.T) {
	items := []sb.Clause{SELECT, UsersID}
	s := sb.Stm(items...)
	items[1] = UsersName
	assertSQL(t, s, "SELECT users.id")
}

// sb.Row and sb.Raw copy theirs too.
func TestRowAndRawCopyTheirInput(t *testing.T) {
	items := []sb.Clause{sb.Id("a"), sb.Id("b")}
	r := sb.Row(items...)
	items[0] = sb.Id("z")
	assertSQL(t, sb.Stm(r), "(a, b)")

	vals := []any{1}
	raw := sb.Raw("x = $0", vals...)
	vals[0] = 99
	assertSQL(t, sb.Stm(raw), "x = $1", 1)
}

// A statement is reusable: building it twice gives the same result, and the args
// of the first build are not carried into the second.
func TestBuildIsRepeatable(t *testing.T) {
	s := sb.Stm(SELECT, UsersID, FROM, Users, WHERE, UsersID, EQ, sb.Lit(1))
	for i := 0; i < 2; i++ {
		assertSQL(t, s, "SELECT users.id FROM users WHERE users.id = $1", 1)
	}
}

// ---------------------------------------------------------------------------
// A sb.Clause implemented outside the package
// ---------------------------------------------------------------------------

// sb.Clause is exported, so a caller can implement a token of their own. Such a
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
			sb.Stm(SELECT, UsersID, jsonPath("{a,b}"), UsersName),
			"SELECT users.id, users.meta #>> $1, users.name",
			[]any{"{a,b}"},
		},
		{
			"ends the item, so the next one takes a comma",
			sb.Stm(SELECT, jsonPath("{a}"), AS, sb.Id("v"), UsersName),
			"SELECT users.meta #>> $1 AS v, users.name",
			[]any{"{a}"},
		},
		{
			"and numbers its own placeholder in expansion order",
			sb.Stm(WHERE, sb.Lit(1), AND, jsonPath("{a}"), EQ, sb.Lit(2)),
			"WHERE $1 AND users.meta #>> $2 = $3",
			[]any{1, "{a}", 2},
		},
	})
}
