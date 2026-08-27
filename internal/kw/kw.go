// Package kw holds the token kinds and the keyword types.
//
// It exists only so that the two public packages can share them: psqlb declares
// the keyword constants and sb reads their kinds while walking a token list.
// The names are exported because both packages need them, and the package is
// internal so that they are not public API. A user therefore cannot name
// ListKw or PrefixKw, and so cannot declare a keyword: a keyword the package
// does not model is written with sb.Op or sb.Kw.
package kw

// Kind is what the walker needs to know about a token: whether it takes a comma
// and what the token after it does. See the package doc of sb.
type Kind int

const (
	KindOperand Kind = iota // begins an item, clears glue
	KindPrefix              // begins an item, sets glue
	KindPostfix             // continues an item, clears glue
	KindInfix               // continues an item, sets glue
	KindList                // opens a comma-separated list
	KindClause              // closes it, back to space-separated
)

// Kinded is implemented by every token that is not an operand. sb.Id,
// sb.Statement, EXCLUDED and any Clause implemented outside these packages do
// not implement it, and are operands: that is the common case, so it is the
// default.
type Kinded interface{ SQLKind() Kind }

// KindOf takes an any rather than an sb.Clause, since sb is the package that
// defines Clause and this one must not import it.
func KindOf(c any) Kind {
	if k, ok := c.(Kinded); ok {
		return k.SQLKind()
	}
	return KindOperand
}

// The keyword types. Their underlying type is string so that the keyword
// constants of package psqlb can be declared with const, which is what lets the
// walker read a token's kind before building it.
type (
	OperandKw string
	PrefixKw  string
	PostfixKw string
	InfixKw   string
	ListKw    string
	ClauseKw  string

	// SetKw is a list keyword that also opens the scope in which a
	// table-qualified column name is emitted bare. SET is its only value.
	SetKw string

	// ExcludedKw consumes the token after it. EXCLUDED is its only value.
	ExcludedKw string
)

func (k OperandKw) SQLKind() Kind { return KindOperand }
func (k PrefixKw) SQLKind() Kind  { return KindPrefix }
func (k PostfixKw) SQLKind() Kind { return KindPostfix }
func (k InfixKw) SQLKind() Kind   { return KindInfix }
func (k ListKw) SQLKind() Kind    { return KindList }
func (k ClauseKw) SQLKind() Kind  { return KindClause }
func (k SetKw) SQLKind() Kind     { return KindList }

func (k OperandKw) BuildSQL(args []any) (string, []any, error) { return string(k), args, nil }
func (k PrefixKw) BuildSQL(args []any) (string, []any, error)  { return string(k), args, nil }
func (k PostfixKw) BuildSQL(args []any) (string, []any, error) { return string(k), args, nil }
func (k InfixKw) BuildSQL(args []any) (string, []any, error)   { return string(k), args, nil }
func (k ListKw) BuildSQL(args []any) (string, []any, error)    { return string(k), args, nil }
func (k ClauseKw) BuildSQL(args []any) (string, []any, error)  { return string(k), args, nil }
func (k SetKw) BuildSQL(args []any) (string, []any, error)     { return string(k), args, nil }

// EXCLUDED and sb.Id satisfy sb.Clause so that they can be written as tokens,
// but the walker intercepts both types before building them: EXCLUDED to read
// the name after it, Id to strip a qualifier inside SET. This method is
// therefore never reached from the walker.
func (k ExcludedKw) BuildSQL(args []any) (string, []any, error) { return string(k), args, nil }
