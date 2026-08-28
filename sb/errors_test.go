package sb_test

import (
	"errors"
	"strings"
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// ===========================================================================
// Rejection
// ===========================================================================
//
// This file has no counterpart in the old engine, and it is the point of the
// redesign: a table of token sequences that must not be rendered. Every case
// here is one that the old walker emitted, either as a string Postgres rejects
// or, worse, as a string Postgres runs and answers wrongly.
//
// Three classes of error are reported, and nothing panics:
//
//   - a syntax error: the token is not legal at this point in the grammar;
//   - a missing token: the position requires something that is not there;
//   - not supported yet: legal PostgreSQL, outside the modelled subset.

type ecase struct {
	name string
	stmt []any
	want string // a substring of the message
}

func runErrs(t *testing.T, cases []ecase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertErr(t, c.stmt, c.want)
		})
	}
}

// A position that SQL always parenthesises requires a group, so the keyword
// that needs one can be a constant like any other. This is what closes the
// "nothing checks that a keyword needing a parenthesised group has one"
// limitation.
func TestMissingGroup(t *testing.T) {
	runErrs(t, []ecase{
		{
			// The old engine produced "IN $1, $2", which Postgres rejects.
			name: "IN without a group",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersStatus, IN, sb.V(1), sb.V(2)),
			want: "a parenthesised list or subquery after IN is required",
		},
		{
			name: "EXISTS without a group",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, EXISTS, sb.V(1)),
			want: "a parenthesised subquery after EXISTS is required",
		},
		{
			name: "ANY without a group",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, EQ, ANY, UsersName),
			want: "a parenthesised subquery or list after ANY is required",
		},
		{
			name: "DISTINCT ON without a group",
			stmt: stmt(SELECT, DISTINCT, ON, UsersID, UsersID),
			want: "a parenthesised list of expressions is required",
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
			stmt: stmt(SELECT, STAR, FROM, sub),
			want: "an alias on a subquery is required",
		},
		{
			name: "alias without AS",
			stmt: stmt(SELECT, STAR, FROM, sub, sb.I("t")),
			want: `write sb.P(...), AS, sb.I("t")`,
		},
	})
}

// Shape is validated: an operand, then an operator, then an operand. Two
// operands in a row are an error wherever the position takes exactly one
// expression.
func TestExpressionShape(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "two operands in WHERE",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, UsersName),
			want: "expected the end of the statement",
		},
		{
			name: "a dangling operator",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, EQ),
			want: "WHERE: expected an expression",
		},
		{
			name: "a dangling AND",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersIsPaid, AND),
			want: "WHERE: expected an expression",
		},
		{
			name: "an empty select list",
			stmt: stmt(SELECT),
			want: "SELECT: expected an expression",
		},
		{
			name: "a keyword where an expression belongs",
			stmt: stmt(SELECT, FROM, Users),
			want: "SELECT: expected an expression, got keyword FROM",
		},
	})
}

// A keyword phrase that is written wrong is reported, naming the word that was
// expected. This is what word-level keywords cost and what the parser gives
// back: the phrase is checked rather than being one opaque constant.
func TestKeywordPhrases(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "ORDER without BY",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, UsersID),
			want: "ORDER BY: expected keyword BY",
		},
		{
			name: "NULLS without FIRST or LAST",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID, NULLS, DESC),
			want: "expected keyword FIRST or LAST",
		},
		{
			name: "IS followed by nothing it accepts",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, IS, sb.V(1)),
			want: "expected keyword NULL, TRUE, FALSE, UNKNOWN or DISTINCT",
		},
		{
			name: "BETWEEN without AND",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersAge, BETWEEN, sb.V(1), sb.V(2)),
			want: "expected keyword AND",
		},
		{
			name: "WHEN without THEN",
			stmt: stmt(SELECT, CASE, WHEN, UsersIsPaid, sb.V(1), END),
			want: "CASE: expected keyword THEN",
		},
		{
			name: "CASE without END",
			stmt: stmt(SELECT, CASE, WHEN, UsersIsPaid, THEN, sb.V(1)),
			want: "CASE: expected keyword END",
		},
	})
}

// A statement is a statement, and a group met where a statement belongs is
// parsed as one.
func TestStatementShape(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "not a statement",
			stmt: stmt(FROM, Users),
			want: "statement: expected keyword SELECT, INSERT, UPDATE or DELETE",
		},
		{
			name: "an alias that is not an identifier",
			stmt: stmt(SELECT, UsersID, AS, sb.V("x")),
			want: "expected an alias written with sb.I",
		},
		{
			name: "a type name that is not an identifier",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersMeta, TYPECAST, sb.V(1)),
			want: "expected a type name written with sb.I or sb.RawExpr",
		},
		{
			name: "a table position that takes no expression",
			stmt: stmt(SELECT, STAR, FROM, sb.V(1)),
			want: "FROM: expected a table name, a subquery or a function call",
		},
	})
}

// Everything the grammar has not reached is reported as such, named, so that a
// user can tell "I wrote this wrong" from "this package has not got there yet".
func TestNotSupportedYet(t *testing.T) {
	cases := []ecase{
		{
			"ORDER BY USING",
			stmt(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID, USING, sb.RawOp(">")),
			"ORDER BY ... USING",
		},
		{
			"ON CONFLICT ON CONSTRAINT",
			stmt(INSERT, INTO, Users, VALUES, sb.P(sb.V(1)), ON, CONFLICT, ON, sb.I("c")),
			"ON CONFLICT ON CONSTRAINT",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := sb.ToSQL(c.stmt...)
			var unsupported *sb.UnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("want *sb.UnsupportedError, got %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error:\n got: %v\nwant substring: %s", err, c.want)
			}
		})
	}
}

// The error types are distinct so that a caller, and a test, can tell them
// apart without reading the message.
func TestErrorTypes(t *testing.T) {
	var syntax *sb.SyntaxError
	_, _, err := sb.ToSQL(FROM, Users)
	if !errors.As(err, &syntax) {
		t.Errorf("want *sb.SyntaxError, got %T: %v", err, err)
	} else if syntax.Pos != 0 || syntax.Production != "statement" {
		t.Errorf("unexpected fields: %#v", syntax)
	}

	var missing *sb.MissingError
	_, _, err = sb.ToSQL(SELECT, STAR, FROM, sb.P(SELECT, UsersID, FROM, Users))
	if !errors.As(err, &missing) {
		t.Errorf("want *sb.MissingError, got %T: %v", err, err)
	}
}

// The write statements are checked the same way. SET is the one position where
// a name is rewritten, so it is also the one that has to say what it expects.
func TestWriteStatements(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "UPDATE without SET",
			stmt: stmt(UPDATE, Users, WHERE, UsersID, EQ, sb.V(1)),
			want: "UPDATE: expected keyword SET",
		},
		{
			name: "an assignment without =",
			stmt: stmt(UPDATE, Users, SET, UsersStatus, sb.V("vip")),
			want: `SET: expected operator "=", written EQ`,
		},
		{
			name: "an assignment target that is not a name",
			stmt: stmt(UPDATE, Users, SET, sb.V(1), EQ, sb.V(2)),
			want: "SET: expected a column name written with sb.I",
		},
		{
			name: "DELETE without FROM",
			stmt: stmt(DELETE, Users),
			want: "DELETE: expected keyword FROM",
		},
		{
			name: "INSERT with neither VALUES nor a query",
			stmt: stmt(INSERT, INTO, Users, sb.P(sb.I("name"))),
			want: "INSERT: expected keyword VALUES or a query",
		},
		{
			name: "VALUES without a group",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.V("a"), sb.V("b")),
			want: "a parenthesised row after VALUES is required",
		},
		{
			name: "ON CONFLICT without DO",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P(sb.V("a")), ON, CONFLICT, NOTHING),
			want: "ON CONFLICT: expected keyword DO",
		},
		{
			name: "DO followed by neither NOTHING nor UPDATE",
			stmt: stmt(INSERT, INTO, Users, VALUES, sb.P(sb.V("a")), ON, CONFLICT, DO, SET),
			want: "ON CONFLICT: expected keyword NOTHING or UPDATE",
		},
	})
}

// A join that PostgreSQL requires a condition on is not emitted without one,
// and one that takes no condition is not emitted with one.
func TestJoinConditions(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "no condition",
			stmt: stmt(SELECT, STAR, FROM, Users, JOIN, Orders),
			want: "JOIN: a join condition is required",
		},
		{
			name: "a condition on a CROSS join",
			stmt: stmt(SELECT, STAR, FROM, Users, CROSS, JOIN, Orders, ON, TRUE),
			want: "expected no condition, since a CROSS or NATURAL join takes none",
		},
		{
			name: "USING without a group",
			stmt: stmt(SELECT, STAR, FROM, Users, JOIN, Orders, USING, sb.I("user_id")),
			want: "a parenthesised list of column names is required",
		},
		{
			name: "a join type with no JOIN",
			stmt: stmt(SELECT, STAR, FROM, Users, LEFT, Orders),
			want: "JOIN: expected keyword JOIN",
		},
		{
			name: "LATERAL on a table",
			stmt: stmt(SELECT, STAR, FROM, LATERAL, Users),
			want: "expected a subquery or a function call after LATERAL",
		},
		{
			name: "a set operation with nothing after it",
			stmt: stmt(SELECT, UsersID, FROM, Users, UNION, ALL),
			want: "statement: expected keyword SELECT",
		},
	})
}

// A window specification, a CTE and a frame clause are checked the same way as
// everything else.
func TestWindowsAndCTEs(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "OVER something that is neither a name nor a definition",
			stmt: stmt(SELECT, sb.F("COUNT", STAR), OVER, sb.V(1), FROM, Users),
			want: "expected a window name or a parenthesised window definition",
		},
		{
			name: "FILTER without WHERE",
			stmt: stmt(SELECT, sb.F("COUNT", STAR), FILTER, sb.P(UsersIsPaid), FROM, Users),
			want: "FILTER: expected keyword WHERE",
		},
		{
			name: "FILTER without a group",
			stmt: stmt(SELECT, sb.F("COUNT", STAR), FILTER, UsersIsPaid, FROM, Users),
			want: "a parenthesised WHERE clause after FILTER is required",
		},
		{
			name: "a frame bound with no direction",
			stmt: stmt(SELECT, sb.F("COUNT", STAR), OVER, sb.P(ROWS, sb.V(3)), FROM, Users),
			want: "frame clause: expected keyword PRECEDING or FOLLOWING",
		},
		{
			name: "BETWEEN in a frame without AND",
			stmt: stmt(SELECT, sb.F("COUNT", STAR),
				OVER, sb.P(ROWS, BETWEEN, UNBOUNDED, PRECEDING, CURRENT, ROW), FROM, Users),
			want: "frame clause: expected keyword AND",
		},
		{
			name: "a WINDOW entry without AS",
			stmt: stmt(SELECT, UsersID, FROM, Users, WINDOW, sb.I("w"), sb.P()),
			want: "WINDOW: expected keyword AS",
		},
		{
			name: "COLLATE without a collation name",
			stmt: stmt(SELECT, UsersID, FROM, Users, ORDER, BY, UsersName, COLLATE, sb.V("C")),
			want: "expected a collation name written with sb.I",
		},
		{
			name: "a CTE without AS",
			stmt: stmt(WITH, sb.I("t"), sb.P(SELECT, UsersID, FROM, Users), SELECT, STAR),
			want: "WITH: expected keyword AS",
		},
		{
			name: "a CTE body that is not a group",
			stmt: stmt(WITH, sb.I("t"), AS, SELECT, UsersID, FROM, Users),
			want: "a parenthesised query as the body is required",
		},
		{
			// A slice passed without "..." is an any like any other and
			// compiles. It would bind the whole statement as one parameter, so
			// normalization makes it a token no production accepts.
			name: "a statement passed as one slice",
			stmt: stmt([]any{SELECT, UsersID, FROM, Users}),
			want: `a slice of 4 items passed without "..."`,
		},
		{
			name: "a fragment passed to sb.P as one slice",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, IN, sb.P([]any{sb.V(1), sb.V(2)})),
			want: `a slice of 2 items passed without "..."`,
		},
		{
			name: "WITH with no statement after it",
			stmt: stmt(WITH, sb.I("t"), AS, sb.P(SELECT, UsersID, FROM, Users)),
			want: "statement: expected keyword SELECT",
		},
	})
}
