package sb

import (
	"fmt"

	"github.com/fujidaiti/psqlb/internal/tok"
	"github.com/fujidaiti/psqlb/kw"
)

// ===========================================================================
// The cursor
// ===========================================================================

// parser reads one group's token list left to right. A nested group gets its
// own parser over its own tokens and shares the emitter, so emission stays in
// token order and $N numbering is unaffected by nesting.
//
// There is no backtracking and never more than two tokens of lookahead. Two are
// needed where a split keyword phrase decides the branch: IS NOT NULL against
// IS NULL, NOT IN against a NOT that is not an infix at all.
type parser struct {
	toks []Token
	pos  int
	e    *emitter
}

func (p *parser) done() bool { return p.pos >= len(p.toks) }

func (p *parser) peek() Token { return p.lookahead(0) }

// lookahead returns nil past the end of the input, and that is the only thing
// a nil means: every token slice came from normalize, which wraps a nil item
// into a value to bind.
func (p *parser) lookahead(n int) Token {
	if p.pos+n >= len(p.toks) {
		return nil
	}
	return p.toks[p.pos+n]
}

// atStar reports whether the next token is the whole-row "*". It is an operator
// token, since "*" is a well-formed operator name and is multiplication in an
// operator position; only the two positions PostgreSQL allows the whole row in
// ask for it here.
func (p *parser) atStar() bool {
	op, ok := p.peek().(tok.Operator)
	return ok && op == star
}

// at reports whether the next token is the keyword k.
func (p *parser) at(k tok.Keyword) bool { return p.atOffset(0, k) }

func (p *parser) atOffset(n int, k tok.Keyword) bool {
	w, ok := p.lookahead(n).(tok.Keyword)
	return ok && w == k
}

// accept consumes k if it is next, without emitting it.
func (p *parser) accept(k tok.Keyword) bool {
	if p.at(k) {
		p.pos++
		return true
	}
	return false
}

// take consumes k and emits it.
func (p *parser) take(k tok.Keyword) bool {
	if p.accept(k) {
		p.e.word(string(k))
		return true
	}
	return false
}

// want consumes and emits k, or reports what was written instead.
func (p *parser) want(prod string, k tok.Keyword) error {
	if p.take(k) {
		return nil
	}
	return p.unexpected(prod, "keyword "+string(k))
}

func (p *parser) unexpected(prod, want string) *SyntaxError {
	return &SyntaxError{Production: prod, Want: want, Got: describe(p.peek()), Pos: p.pos}
}

// unexpectedOperand reports a token that cannot begin an operand, and names the
// way out when the token is one normalization produced from a plain Go value.
// An operator here is the misplacement this package is meant to catch —
// ToSQL(SELECT, "=") — and it has two shapes: the string was an operator in the
// wrong place, or it was a value the lexical rule took for an operator.
func (p *parser) unexpectedOperand(prod, want string) error {
	e := p.unexpected(prod, want)
	switch t := p.peek().(type) {
	case tok.Operator:
		e.Fix = fmt.Sprintf("sb.Arg(%q) if that string was meant as a value", string(t))
	case unspread:
		e.Fix = `the slice with "..."`
	}
	return e
}

// describe names a token the way it is written, so that an error message can be
// read next to the Go source that produced it.
func describe(t Token) string {
	switch v := t.(type) {
	case nil:
		return "the end of the group"
	case tok.Keyword:
		return "keyword " + string(v)
	case tok.Operator:
		return fmt.Sprintf("operator %q", string(v))
	case I:
		return fmt.Sprintf("identifier %q", string(v))
	case value:
		return "a value"
	case rawFrag:
		return "a raw fragment"
	case unspread:
		return fmt.Sprintf("a slice of %d items passed without %q", v.n, "...")
	case Group:
		if v.name != "" {
			return "a call of " + v.name
		}
		return "a group"
	default:
		return fmt.Sprintf("%T", t)
	}
}

// startsExpr reports whether the next token can begin an expression. It is how
// a comma-separated list finds its own end: an item is parsed until its grammar
// is complete, and what follows either begins another item or does not belong
// to the list at all. A keyword that is not an expression on its own therefore
// ends the list, with no list of clause keywords to keep in step.
func (p *parser) startsExpr() bool {
	switch t := p.peek().(type) {
	case I, value, rawFrag, Group:
		return true
	case tok.Keyword:
		switch t {
		case kw.NOT, kw.EXISTS, kw.CASE, kw.TRUE, kw.FALSE, kw.NULL, kw.DEFAULT:
			return true
		}
	}
	return false
}

// ===========================================================================
// Groups
// ===========================================================================

// parens runs f over the tokens of g, between parentheses. Every nested group
// is parenthesised, empty or not; the outermost level never is. This is the
// only place a parenthesis is written.
func (p *parser) parens(g Group, f func(*parser) error) error {
	sub := &parser{toks: g.items, e: p.e}
	p.e.open(g.name)
	if err := f(sub); err != nil {
		return err
	}
	if !sub.done() {
		return sub.unexpected("group", "the end of the group")
	}
	p.e.close()
	return nil
}

// group consumes the next token as a plain group, or reports that the position
// requires one. SQL parenthesises here, so the DSL demands a P here.
func (p *parser) group(prod, want, fix string) (Group, error) {
	g, ok := p.peek().(Group)
	if !ok || g.name != "" {
		return Group{}, &MissingError{Production: prod, Want: want, Fix: fix}
	}
	p.pos++
	return g, nil
}

// startsStatement reports whether a group is a statement rather than a list of
// expressions. It is the one place a group is looked into before being parsed,
// and it looks only at the first token.
func startsStatement(g Group) bool {
	if len(g.items) == 0 {
		return false
	}
	k, ok := g.items[0].(tok.Keyword)
	if !ok {
		return false
	}
	switch k {
	case kw.SELECT, kw.WITH, kw.VALUES, kw.INSERT, kw.UPDATE, kw.DELETE:
		return true
	}
	return false
}

// ===========================================================================
// Statements
// ===========================================================================

// statement parses one complete statement.
func (p *parser) statement() error {
	switch {
	case p.at(kw.INSERT):
		return p.insertStmt()
	case p.at(kw.UPDATE):
		return p.updateStmt()
	case p.at(kw.DELETE):
		return p.deleteStmt()
	case p.at(kw.WITH):
		return p.withStmt()
	}
	return p.query()
}
