package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// ===========================================================================
// sb.RawExpr — the edges of $0 substitution
// ===========================================================================

func TestRawTooFewValues(t *testing.T) {
	// Leaving the marker in place would produce "b = $0", which Postgres rejects
	// with "there is no parameter $0", so this is an error.
	assertErr(t,
		stmt(SELECT, "*", FROM, Users, WHERE, sb.RawExpr("a = $0 AND b = $0", 1)),
		"2 $0 marker(s) but 1 value(s)",
	)
}

func TestRawSurplusValues(t *testing.T) {
	// Binding the surplus would leave the statement with a parameter nothing
	// refers to, and Postgres rejects the count mismatch, so this is an error
	// too.
	assertErr(t,
		stmt(SELECT, "*", FROM, Users, WHERE, sb.RawExpr("a = $0", 1, 2)),
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
			assertErr(t, stmt(SELECT, "*", FROM, Users, WHERE, sb.RawExpr(fragment)), "only $0 marks a value")
		})
	}
}
