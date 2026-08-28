package sb

import (
	"fmt"

	"github.com/fujidaiti/psqlb/internal/kw"
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

func (p *parser) lookahead(n int) Token {
	if p.pos+n >= len(p.toks) {
		return nil
	}
	return p.toks[p.pos+n]
}

// at reports whether the next token is the keyword k.
func (p *parser) at(k kw.Keyword) bool { return p.atOffset(0, k) }

func (p *parser) atOffset(n int, k kw.Keyword) bool {
	w, ok := p.lookahead(n).(kw.Keyword)
	return ok && w == k
}

// accept consumes k if it is next, without emitting it.
func (p *parser) accept(k kw.Keyword) bool {
	if p.at(k) {
		p.pos++
		return true
	}
	return false
}

// take consumes k and emits it.
func (p *parser) take(k kw.Keyword) bool {
	if p.accept(k) {
		p.e.word(string(k))
		return true
	}
	return false
}

// want consumes and emits k, or reports what was written instead.
func (p *parser) want(prod string, k kw.Keyword) error {
	if p.take(k) {
		return nil
	}
	return p.unexpected(prod, "keyword "+string(k))
}

func (p *parser) unexpected(prod, want string) error {
	return &SyntaxError{Production: prod, Want: want, Got: describe(p.peek()), Pos: p.pos}
}

// describe names a token the way it is written, so that an error message can be
// read next to the Go source that produced it.
func describe(t Token) string {
	switch v := t.(type) {
	case nil:
		return "the end of the group"
	case kw.Keyword:
		return "keyword " + string(v)
	case kw.Operator:
		return fmt.Sprintf("operator %q", string(v))
	case Id:
		return fmt.Sprintf("identifier %q", string(v))
	case value:
		return "a value"
	case rawFrag:
		return "a raw fragment"
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
	case Id, value, rawFrag, Group:
		return true
	case kw.Keyword:
		switch t {
		case kw.NOT, kw.EXISTS, kw.CASE, kw.STAR, kw.TRUE, kw.FALSE, kw.NULL, kw.DEFAULT:
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
	sub := &parser{toks: compact(g.items), e: p.e}
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
// requires one. SQL parenthesises here, so the DSL demands an S here.
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
	items := compact(g.items)
	if len(items) == 0 {
		return false
	}
	k, ok := items[0].(kw.Keyword)
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

// statement parses one complete statement. Only SELECT is modelled so far.
func (p *parser) statement() error {
	switch {
	case p.at(kw.SELECT):
		return p.selectStmt()
	case p.at(kw.WITH):
		return &UnsupportedError{"WITH"}
	case p.at(kw.INSERT):
		return &UnsupportedError{"INSERT"}
	case p.at(kw.UPDATE):
		return &UnsupportedError{"UPDATE"}
	case p.at(kw.DELETE):
		return &UnsupportedError{"DELETE"}
	case p.at(kw.VALUES):
		return &UnsupportedError{"a bare VALUES statement"}
	}
	return p.unexpected("statement", "keyword SELECT")
}

// selectStmt implements, of the SELECT synopsis:
//
//	SELECT [ ALL | DISTINCT [ ON ( expression [, ...] ) ] ]
//	    expression [ AS output_name ] [, ...]
//	    [ FROM from_item [, ...] ]
//	    [ WHERE condition ]
//	    [ ORDER BY expression [ ASC | DESC ] [ NULLS { FIRST | LAST } ] [, ...] ]
//	    [ LIMIT { count | ALL } ]
//	    [ OFFSET start ]
func (p *parser) selectStmt() error {
	p.take(kw.SELECT)

	if !p.take(kw.ALL) && p.take(kw.DISTINCT) && p.take(kw.ON) {
		g, err := p.group("DISTINCT ON", "a parenthesised list of expressions", "DISTINCT, ON, sb.S(...)")
		if err != nil {
			return err
		}
		if err := p.parens(g, (*parser).exprList); err != nil {
			return err
		}
	}

	if err := p.selectList(); err != nil {
		return err
	}
	if p.take(kw.FROM) {
		if err := p.fromList(); err != nil {
			return err
		}
	}
	if p.take(kw.WHERE) {
		if err := p.expr("WHERE"); err != nil {
			return err
		}
	}
	switch {
	case p.at(kw.GROUP):
		return &UnsupportedError{"GROUP BY"}
	case p.at(kw.HAVING):
		return &UnsupportedError{"HAVING"}
	case p.at(kw.WINDOW):
		return &UnsupportedError{"the WINDOW clause"}
	}
	if p.take(kw.ORDER) {
		if err := p.want("ORDER BY", kw.BY); err != nil {
			return err
		}
		if err := p.sortList(); err != nil {
			return err
		}
	}
	if p.take(kw.LIMIT) && !p.take(kw.ALL) {
		if err := p.expr("LIMIT"); err != nil {
			return err
		}
	}
	if p.take(kw.OFFSET) {
		if err := p.expr("OFFSET"); err != nil {
			return err
		}
	}
	switch {
	case p.at(kw.UNION), p.at(kw.INTERSECT), p.at(kw.EXCEPT):
		return &UnsupportedError{"set operations"}
	}
	return nil
}

// selectList parses the output list.
//
//	expression [ AS output_name ] [, ...]
//
// The comma is written by this production, which is the whole reason the
// package exists. An alias must be written with AS: an identifier standing on
// its own is the next item of the list, never an alias for the one before it.
func (p *parser) selectList() error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		if err := p.expr("SELECT"); err != nil {
			return err
		}
		if p.take(kw.AS) {
			if err := p.alias("SELECT"); err != nil {
				return err
			}
		}
		if !p.startsExpr() {
			return nil
		}
	}
}

func (p *parser) alias(prod string) error {
	id, ok := p.peek().(Id)
	if !ok {
		return p.unexpected(prod, "an alias written with sb.Id")
	}
	p.pos++
	p.e.word(string(id))
	return nil
}

// fromList parses the FROM list.
//
//	from_item [, ...]
func (p *parser) fromList() error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		if err := p.fromItem(); err != nil {
			return err
		}
		switch {
		case p.at(kw.JOIN), p.at(kw.LEFT), p.at(kw.RIGHT), p.at(kw.FULL),
			p.at(kw.INNER), p.at(kw.CROSS), p.at(kw.NATURAL), p.at(kw.LATERAL):
			return &UnsupportedError{"JOIN"}
		}
		if !p.startsExpr() {
			return nil
		}
	}
}

// fromItem implements, of the from_item synopsis:
//
//	table_name [ AS alias ]
//	( select ) AS alias
//	function_name ( argument [, ...] ) [ AS alias ]
//
// The alias on a subquery is required by PostgreSQL, so its absence is reported
// here rather than by the server. That is the one requirement of this kind the
// grammar can state: whether a name in a list is an alias or the next item
// cannot be known, so an alias is always written with AS.
func (p *parser) fromItem() error {
	subquery := false
	switch t := p.peek().(type) {
	case Id:
		p.pos++
		p.e.word(string(t))
	case Group:
		p.pos++
		if t.name != "" {
			if err := p.call(t); err != nil {
				return err
			}
		} else {
			subquery = true
			if err := p.parens(t, (*parser).statement); err != nil {
				return err
			}
		}
	default:
		return p.unexpected("FROM", "a table name, a subquery or a function call")
	}

	if p.take(kw.AS) {
		return p.alias("FROM")
	}
	if subquery {
		return &MissingError{
			Production: "FROM",
			Want:       "an alias on a subquery",
			Fix:        `sb.S(...), AS, sb.Id("t")`,
		}
	}
	return nil
}

// sortList parses the ORDER BY list.
//
//	expression [ ASC | DESC ] [ NULLS { FIRST | LAST } ] [, ...]
//
// The sort key consumes its own modifiers, so the comma falls where the next
// key begins and no token needs to be marked as attaching to the one before it.
func (p *parser) sortList() error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		if err := p.expr("ORDER BY"); err != nil {
			return err
		}
		if !p.take(kw.ASC) {
			p.take(kw.DESC)
		}
		if p.take(kw.NULLS) && !p.take(kw.FIRST) && !p.take(kw.LAST) {
			return p.unexpected("ORDER BY", "keyword FIRST or LAST")
		}
		if p.at(kw.USING) {
			return &UnsupportedError{"ORDER BY ... USING"}
		}
		if !p.startsExpr() {
			return nil
		}
	}
}
