package psqlb

import (
	"errors"
	"strings"
	"testing"

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
	stmt sb.Group
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
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersStatus, IN, sb.Lit(1), sb.Lit(2)),
			want: "a parenthesised list or subquery after IN is required",
		},
		{
			name: "EXISTS without a group",
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, EXISTS, sb.Lit(1)),
			want: "a parenthesised subquery after EXISTS is required",
		},
		{
			name: "ANY without a group",
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersID, EQ, ANY, UsersName),
			want: "a parenthesised subquery or list after ANY is required",
		},
		{
			name: "DISTINCT ON without a group",
			stmt: sb.S(SELECT, DISTINCT, ON, UsersID, UsersID),
			want: "a parenthesised list of expressions is required",
		},
	})
}

// PostgreSQL requires an alias on a subquery in FROM, and requires it to be
// written after the subquery. Both halves are reported here rather than by the
// server. Nothing else about aliases is guessed: see TestSelectList.
func TestSubqueryAlias(t *testing.T) {
	sub := sb.S(SELECT, UsersID, FROM, Users)
	runErrs(t, []ecase{
		{
			name: "no alias",
			stmt: sb.S(SELECT, STAR, FROM, sub),
			want: "an alias on a subquery is required",
		},
		{
			name: "alias without AS",
			stmt: sb.S(SELECT, STAR, FROM, sub, sb.Id("t")),
			want: `write sb.S(...), AS, sb.Id("t")`,
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
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersID, UsersName),
			want: "expected the end of the statement",
		},
		{
			name: "a dangling operator",
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersID, EQ),
			want: "WHERE: expected an expression",
		},
		{
			name: "a dangling AND",
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersIsPaid, AND),
			want: "WHERE: expected an expression",
		},
		{
			name: "an empty select list",
			stmt: sb.S(SELECT),
			want: "SELECT: expected an expression",
		},
		{
			name: "a keyword where an expression belongs",
			stmt: sb.S(SELECT, FROM, Users),
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
			stmt: sb.S(SELECT, UsersID, FROM, Users, ORDER, UsersID),
			want: "ORDER BY: expected keyword BY",
		},
		{
			name: "NULLS without FIRST or LAST",
			stmt: sb.S(SELECT, UsersID, FROM, Users, ORDER, BY, UsersID, NULLS, DESC),
			want: "expected keyword FIRST or LAST",
		},
		{
			name: "IS followed by nothing it accepts",
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersID, IS, sb.Lit(1)),
			want: "expected keyword NULL, TRUE, FALSE, UNKNOWN or DISTINCT",
		},
		{
			name: "BETWEEN without AND",
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersAge, BETWEEN, sb.Lit(1), sb.Lit(2)),
			want: "expected keyword AND",
		},
		{
			name: "WHEN without THEN",
			stmt: sb.S(SELECT, CASE, WHEN, UsersIsPaid, sb.Lit(1), END),
			want: "CASE: expected keyword THEN",
		},
		{
			name: "CASE without END",
			stmt: sb.S(SELECT, CASE, WHEN, UsersIsPaid, THEN, sb.Lit(1)),
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
			stmt: sb.S(FROM, Users),
			want: "statement: expected keyword SELECT",
		},
		{
			name: "an alias that is not an identifier",
			stmt: sb.S(SELECT, UsersID, AS, sb.Lit("x")),
			want: "expected an alias written with sb.Id",
		},
		{
			name: "a type name that is not an identifier",
			stmt: sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersMeta, TYPECAST, sb.Lit(1)),
			want: "expected a type name written with sb.Id or sb.Raw",
		},
		{
			name: "a table position that takes no expression",
			stmt: sb.S(SELECT, STAR, FROM, sb.Lit(1)),
			want: "FROM: expected a table name, a subquery or a function call",
		},
	})
}

// Everything the grammar has not reached is reported as such, named, so that a
// user can tell "I wrote this wrong" from "this package has not got there yet".
func TestNotSupportedYet(t *testing.T) {
	cases := []ecase{
		{"GROUP BY", sb.S(SELECT, UsersID, FROM, Users, GROUP, BY, UsersID), "GROUP BY"},
		{"HAVING", sb.S(SELECT, UsersID, FROM, Users, HAVING, UsersID), "HAVING"},
		{"JOIN", sb.S(SELECT, UsersID, FROM, Users, JOIN, Orders), "JOIN"},
		{"set operations", sb.S(SELECT, UsersID, FROM, Users, UNION, ALL), "set operations"},
		{"WITH", sb.S(WITH, sb.Id("t"), AS, sb.S(SELECT, UsersID, FROM, Users)), "WITH"},
		{"INSERT", sb.S(INSERT, INTO, Users), "INSERT"},
		{"UPDATE", sb.S(UPDATE, Users), "UPDATE"},
		{"DELETE", sb.S(DELETE, FROM, Users), "DELETE"},
		{"FILTER", sb.S(SELECT, sb.Func("COUNT", STAR), FILTER, sb.S(WHERE, UsersIsPaid), FROM, Users), "FILTER"},
		{"OVER", sb.S(SELECT, sb.Func("SUM", UsersAge), OVER, sb.S(), FROM, Users), "OVER"},
		{"COLLATE", sb.S(SELECT, UsersID, FROM, Users, WHERE, UsersName, COLLATE, sb.Id("c")), "COLLATE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := c.stmt.ToSQL()
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
	_, _, err := sb.S(FROM, Users).ToSQL()
	if !errors.As(err, &syntax) {
		t.Errorf("want *sb.SyntaxError, got %T: %v", err, err)
	} else if syntax.Pos != 0 || syntax.Production != "statement" {
		t.Errorf("unexpected fields: %#v", syntax)
	}

	var missing *sb.MissingError
	_, _, err = sb.S(SELECT, STAR, FROM, sb.S(SELECT, UsersID, FROM, Users)).ToSQL()
	if !errors.As(err, &missing) {
		t.Errorf("want *sb.MissingError, got %T: %v", err, err)
	}
}
