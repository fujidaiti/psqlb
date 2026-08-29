package sb_test

import (
	"strings"
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// ===========================================================================
// Normalization: which plain Go values become operators
// ===========================================================================

// A string is an operator when it is a well-formed PostgreSQL operator name,
// and a value to bind otherwise. The rule is the one PostgreSQL's own lexer
// uses (reference manual 4.1.3), so no list of operators is kept and an
// extension operator needs nothing special.
//
// The two tables together are the pass-through invariant: the first is every
// shape of string that reaches the SQL text, and the second proves that a
// keyword-shaped or identifier-shaped string is bound instead of emitted.
func TestOperatorNames(t *testing.T) {
	// Emitted verbatim, binding nothing.
	for _, op := range []string{
		"=", "<>", "!=", ">=", "<=", ">", "<",
		"@>", "<@", "<->", "->>", "#>>", "||", "~", "!~*",
		"+", "-", "*", "/", "%",
		"@-", "#-", // a trailing "-" is allowed with a special character present
		strings.Repeat("@", 63), // the length cap, NAMEDATALEN-1
	} {
		t.Run("operator "+op, func(t *testing.T) {
			assertSQL(t,
				stmt(SELECT, "*", FROM, Users, WHERE, UsersID, op, UsersName),
				"SELECT * FROM users WHERE users.id "+op+" users.name",
			)
		})
	}

	// "::" is neither: ":" is not an operator character, so it cannot come
	// through the rule, and normalization maps it to the typecast keyword.
	t.Run(`typecast "::"`, func(t *testing.T) {
		assertSQL(t,
			stmt(SELECT, UsersID, FROM, Users, WHERE, UsersMeta, "@>", `{}`, "::", sb.I("jsonb")),
			"SELECT users.id FROM users WHERE users.meta @> $1::jsonb",
			`{}`,
		)
	})

	// Bound as a parameter. Each one fails the rule for a different reason.
	for _, name := range []string{
		"",                 // would vanish from the output: emitter.word drops it
		"--", "-- x", "/*", // either sequence opens a comment
		"<-", "=-", // a trailing "-" with no special character
		strings.Repeat("@", 64), // one over the length cap
		",", "(", ")",           // punctuation that is not an operator character
		"a%", "NULL", "id", // a character outside the set
		"users.id", "1", "count(*)",
	} {
		t.Run("value "+name, func(t *testing.T) {
			assertSQL(t,
				stmt(SELECT, "*", FROM, Users, WHERE, UsersID, "=", name),
				"SELECT * FROM users WHERE users.id = $1",
				name,
			)
		})
	}
}

// An operator is the one thing a caller writes that reaches the SQL text
// verbatim, so it is the one place a string could carry SQL of its own. It
// cannot: a string is emitted only when every byte of it is an operator
// character, which leaves out the letter, the digit, the space, the quote, the
// parenthesis, the comma and the semicolon, and "--" and "/*" are rejected
// outright. A payload therefore fails the rule, binds as a parameter, and is
// then met by a production that wanted an operator.
//
// This says nothing about sb.I and sb.RawExpr, which are emitted as written
// and are documented as the two places a caller must not put untrusted input.
func TestOperatorPositionIsNotInjectable(t *testing.T) {
	for _, payload := range []string{
		"= 0 OR 1 = 1 OR 0 =",       // the shape this test exists for
		"=0OR1=1OR0=",               // the same with no space to fail on
		"= 1; DROP TABLE users; --", // a second statement
		"= users.id --",             // commenting out the rest of the clause
		"'", "''", `"`,              // a quote, to break out of a literal
		") OR (", "(1=1)", // a parenthesis, to break out of a group
		"= ANY", "IS NOT NULL", // SQL written as a word rather than a symbol
	} {
		t.Run(payload, func(t *testing.T) {
			assertErr(t,
				stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, payload, 1),
				"expected the end of the statement, got a value")
		})
	}

	// What does survive the rule is a single operator token and nothing more.
	// "*/" is a well-formed operator name — the rule forbids "/*" and not its
	// reverse — and is harmless for the same reason the rest are: no fragment
	// this package emits can open the comment it would close.
	for _, name := range []string{"*/", "=", "!!!"} {
		t.Run("survives "+name, func(t *testing.T) {
			assertSQL(t,
				stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, name, 1),
				"SELECT users.id FROM users WHERE users.id "+name+" $1",
				1)
		})
	}
}

// A string is decided by the lexical rule before the position is known, so an
// operator can land where the grammar wants something else. Every such position
// reports it, naming what it wanted rather than what an operator is. The sb.Arg
// hint is attached at one position only — the one that wanted an expression —
// because that is the only place where "you meant a value" is the likely
// mistake: binding a parameter is not legal where a table name belongs.
func TestMisplacedOperators(t *testing.T) {
	runErrs(t, []ecase{
		{
			// The reason an operator in an operand position stays an error: it
			// is what catches a misplaced one, since a string is decided by the
			// lexical rule before the position is known.
			name: "an operator where the select list begins",
			stmt: stmt(SELECT, "=", FROM, Users),
			want: `expected an expression, got operator "="`,
		},
		{
			// "*" is a well-formed operator name, so it is multiplication in an
			// operator position and the whole row only where PostgreSQL allows
			// the whole row.
			name: "the whole-row star in an operand position",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, "=", "*"),
			want: "WHERE: expected an expression",
		},
		{
			// The hint is worded as a condition and not an instruction: here
			// the fix is sb.Arg, and for a misplaced operator it is to move the
			// operator.
			name: "a LIKE pattern that lexes as an operator",
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersName, LIKE, "%"),
			want: `write sb.Arg("%") if that string was meant as a value`,
		},
		{
			name: "an operator where a FROM item belongs",
			stmt: stmt(SELECT, UsersID, FROM, "@>"),
			want: "FROM: expected a table name, a subquery or a function call, got operator",
		},
		{
			name: "an operator where the UPDATE target belongs",
			stmt: stmt(UPDATE, "@>", SET, UsersStatus, "=", 1),
			want: `UPDATE: expected a table name written with sb.I, got operator "@>"`,
		},
		{
			name: "an operator where the INSERT target belongs",
			stmt: stmt(INSERT, INTO, "@>", sb.P(UsersStatus), VALUES, sb.P(1)),
			want: `INSERT: expected a table name written with sb.I, got operator "@>"`,
		},
		{
			name: "an operator where the DELETE target belongs",
			stmt: stmt(DELETE, FROM, "@>"),
			want: `DELETE: expected a table name written with sb.I, got operator "@>"`,
		},
		{
			name: "an operator where an alias belongs",
			stmt: stmt(SELECT, UsersID, AS, "@>", FROM, Users),
			want: `SELECT: expected an alias written with sb.I, got operator "@>"`,
		},
		{
			name: "an operator where a CTE name belongs",
			stmt: stmt(WITH, "@>", AS, sb.P(SELECT, UsersID, FROM, Users), SELECT, "*"),
			want: `WITH: expected a query name written with sb.I, got operator "@>"`,
		},
		{
			name: "an operator where a window belongs",
			stmt: stmt(SELECT, sb.F("COUNT", "*"), OVER, "@>", FROM, Users),
			want: "expected a window name or a parenthesised window definition, got operator",
		},
	})
}

// A slice passed without "..." compiles, since a slice is an any, and would
// otherwise bind the whole statement as one parameter. Normalization makes it a
// token no production accepts.
func TestSliceWithoutSpread(t *testing.T) {
	runErrs(t, []ecase{
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
			stmt: stmt(SELECT, UsersID, FROM, Users, WHERE, UsersID, IN, sb.P([]any{1, 2})),
			want: `a slice of 2 items passed without "..." (token 0); write the slice with "..."`,
		},
	})
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
		stmt(SELECT, UsersID, FROM, Users, WHERE, UsersStatus, "=", nil),
		"SELECT users.id FROM users WHERE users.status = $1",
		nil,
	)
}
