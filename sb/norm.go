package sb

import (
	"strings"

	"github.com/fujidaiti/psqlb/internal/tok"
)

// Normalization is the boundary between plain Go values and tokens. It runs at
// the three places a caller hands items in — ToSQL, P and F — and nowhere
// else, so every []Token in this package was produced here.
//
// It is the one decision the package takes before the position is known, and
// it is lexical: a string that is a well-formed PostgreSQL operator name is an
// operator, and everything else is a value to bind. Arg is the override, for a
// value whose Go string would otherwise lex as an operator.

// The characters PostgreSQL allows in an operator name, and the subset that a
// multi-character name ending in "+" or "-" must contain (reference manual
// 4.1.3). Both are double-quoted because a raw string literal cannot hold the
// backtick.
const (
	operatorChars = "+-*/<>=~!@#%^&|`?"
	specialChars  = "~!@#%^&|`?"
	maxOperator   = 63 // NAMEDATALEN-1
)

// normalize turns the items a caller wrote into tokens. It always allocates,
// which is what keeps a statement from changing when the caller mutates the
// slice it was built from.
func normalize(items []any) []Token {
	out := make([]Token, len(items))
	for i, item := range items {
		out[i] = asToken(item)
	}
	return out
}

// asToken decides what one item is. The order matters: a token stays what it
// is, a string that lexes as an operator is one, and anything else — nil
// included — is a value to bind.
func asToken(item any) Token {
	switch v := item.(type) {
	case Token:
		return v

	case string:
		if isOperator(v) {
			return tok.Operator(v)
		}
		return value{v}

	case []any:
		return unspread{len(v)}

	case []Token:
		return unspread{len(v)}

	default:
		return value{item}
	}
}

// isOperator reports whether s is a well-formed PostgreSQL operator name. It
// says nothing about whether such an operator exists: the rule is lexical, so
// "==" is well-formed and PostgreSQL rejects it at execution.
func isOperator(s string) bool {
	// The empty string would satisfy the character rule vacuously, and
	// emitter.word drops an empty fragment, so the token would disappear from
	// the output with no error. It is a value.
	if s == "" || len(s) > maxOperator {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(operatorChars, s[i]) < 0 {
			return false
		}
	}
	// Either sequence would open a comment.
	if strings.Contains(s, "--") || strings.Contains(s, "/*") {
		return false
	}
	// A multi-character name ending in "+" or "-" needs a character that
	// cannot end one, so that "x*-y" reads as two operators and not one.
	if len(s) > 1 {
		if last := s[len(s)-1]; last == '+' || last == '-' {
			return strings.ContainsAny(s, specialChars)
		}
	}
	return true
}

// unspread is what a []any or a []Token becomes: a slice passed without "...".
// It exists because that mistake compiles — a slice is an any — and would
// otherwise bind the whole statement as one parameter. No production accepts
// it, so it is always reported, and describe names the fix.
type unspread struct{ n int }

func (unspread) SQLToken() {}
