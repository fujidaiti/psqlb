package sb

import "github.com/fujidaiti/psqlb/internal/kw"

// Expressions are parsed without precedence. The DSL requires explicit
// parentheses and the emitter never adds or removes any, so operators are
// emitted in the order they are written and no tree is built. What is validated
// is shape: an operand, then an operator, then an operand. WHERE, a, b is an
// error; WHERE, a, AND, b is not.

// expr parses one expression. prod names the clause it belongs to, so that an
// error can say where it was written.
func (p *parser) expr(prod string) error {
	if err := p.operand(prod); err != nil {
		return err
	}
	for {
		more, err := p.infix(prod)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

// exprList parses a comma-separated list of expressions: the items of a row
// constructor, an IN list, a DISTINCT ON list or a function's arguments.
func (p *parser) exprList() error { return p.commaExprs("expression list") }

// commaExprs is the list loop the expression lists share. The comma comes from
// here, and the list ends where the next token cannot begin an expression.
func (p *parser) commaExprs(prod string) error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		if err := p.expr(prod); err != nil {
			return err
		}
		if !p.startsExpr() {
			return nil
		}
	}
}

// infix parses whatever continues the expression, and reports whether anything
// did. A token that cannot continue one ends the expression, which is what lets
// the production above decide whether it begins the next list item or a clause.
func (p *parser) infix(prod string) (bool, error) {
	switch t := p.peek().(type) {
	case kw.Operator:
		p.pos++
		p.e.word(string(t))
		return true, p.quantified(prod)

	case kw.Keyword:
		switch t {
		case kw.AND, kw.OR, kw.LIKE, kw.ILIKE:
			p.pos++
			p.e.word(string(t))
			return true, p.operand(prod)

		case kw.TYPECAST:
			// x::jsonb is written with no spaces around the operator, so the
			// type name is emitted glued to it.
			p.pos++
			p.e.glue("::")
			p.e.noSpace()
			return true, p.typeName(prod)

		case kw.IS:
			return true, p.isPredicate(prod)

		case kw.IN:
			return true, p.inPredicate(prod)

		case kw.BETWEEN:
			return true, p.betweenPredicate(prod)

		case kw.NOT:
			// NOT continues an expression only as part of a negated predicate.
			// Anywhere else it begins one, and the caller decides what that
			// means.
			switch {
			case p.atOffset(1, kw.IN):
				p.pos++
				p.e.word("NOT")
				return true, p.inPredicate(prod)
			case p.atOffset(1, kw.BETWEEN):
				p.pos++
				p.e.word("NOT")
				return true, p.betweenPredicate(prod)
			case p.atOffset(1, kw.LIKE), p.atOffset(1, kw.ILIKE):
				p.pos++
				p.e.word("NOT")
				w := p.peek().(kw.Keyword)
				p.pos++
				p.e.word(string(w))
				return true, p.operand(prod)
			case p.atOffset(1, kw.SIMILAR):
				p.pos++
				p.e.word("NOT")
				return true, p.similarTo(prod)
			}

		case kw.COLLATE:
			p.pos++
			p.e.word("COLLATE")
			name, ok := p.peek().(Id)
			if !ok {
				return true, p.unexpected(prod, "a collation name written with sb.Id")
			}
			p.pos++
			p.e.word(string(name))
			return true, nil

		case kw.SIMILAR:
			return true, p.similarTo(prod)

		case kw.FILTER:
			return true, p.filter(prod)

		case kw.OVER:
			return true, p.over(prod)
		}
	}
	return false, nil
}

// quantified parses the right-hand side of an operator.
//
//	expression operator [ ANY | SOME | ALL ] ( subquery )
//	expression operator expression
func (p *parser) quantified(prod string) error {
	var q kw.Keyword
	switch {
	case p.at(kw.ANY):
		q = kw.ANY
	case p.at(kw.SOME):
		q = kw.SOME
	case p.at(kw.ALL):
		q = kw.ALL
	default:
		return p.operand(prod)
	}
	p.take(q)
	g, err := p.group(prod,
		"a parenthesised subquery or list after "+string(q),
		string(q)+", sb.S(...)")
	if err != nil {
		return err
	}
	return p.parenExpr(g)
}

// isPredicate implements
//
//	expression IS [ NOT ] NULL | TRUE | FALSE | UNKNOWN
//	expression IS [ NOT ] DISTINCT FROM expression
func (p *parser) isPredicate(prod string) error {
	p.take(kw.IS)
	p.take(kw.NOT)
	switch {
	case p.take(kw.NULL), p.take(kw.TRUE), p.take(kw.FALSE), p.take(kw.UNKNOWN):
		return nil
	case p.take(kw.DISTINCT):
		if err := p.want(prod, kw.FROM); err != nil {
			return err
		}
		return p.operand(prod)
	}
	return p.unexpected(prod, "keyword NULL, TRUE, FALSE, UNKNOWN or DISTINCT")
}

// inPredicate implements
//
//	expression [ NOT ] IN ( expression [, ...] )
//	expression [ NOT ] IN ( subquery )
//
// The parentheses are always there in SQL, so a group is required here. This is
// what makes IN, Lit(1), Lit(2) an error rather than the invalid IN $1, $2.
func (p *parser) inPredicate(prod string) error {
	p.take(kw.IN)
	g, err := p.group(prod, "a parenthesised list or subquery after IN", "IN, sb.S(...)")
	if err != nil {
		return err
	}
	return p.parenExpr(g)
}

// betweenPredicate implements
//
//	expression [ NOT ] BETWEEN expression AND expression
//
// The AND belongs to BETWEEN and is consumed here, so it does not read as the
// boolean operator.
func (p *parser) betweenPredicate(prod string) error {
	p.take(kw.BETWEEN)
	if err := p.operand(prod); err != nil {
		return err
	}
	if err := p.want(prod, kw.AND); err != nil {
		return err
	}
	return p.operand(prod)
}

// operand parses one complete operand, including any prefix it carries.
func (p *parser) operand(prod string) error {
	switch t := p.peek().(type) {
	case Id:
		p.pos++
		p.e.word(string(t))
		return nil

	case value:
		p.pos++
		p.e.bind(t.v)
		return nil

	case rawFrag:
		// A hand-written fragment is one opaque expression. The parser does not
		// look inside it, so where it may appear is checked and what it
		// contains is not.
		p.pos++
		if t.err != nil {
			return t.err
		}
		p.e.rawFragment(t.parts, t.vals)
		return nil

	case Group:
		p.pos++
		if t.name != "" {
			return p.call(t)
		}
		return p.parenExpr(t)

	case kw.Keyword:
		switch t {
		case kw.STAR, kw.TRUE, kw.FALSE, kw.NULL, kw.DEFAULT:
			p.pos++
			p.e.word(string(t))
			return nil

		case kw.NOT:
			p.pos++
			p.e.word("NOT")
			return p.operand(prod)

		case kw.EXISTS:
			p.pos++
			p.e.word("EXISTS")
			g, err := p.group(prod,
				"a parenthesised subquery after EXISTS", "EXISTS, sb.S(SELECT, ...)")
			if err != nil {
				return err
			}
			return p.parens(g, (*parser).statement)

		case kw.CASE:
			return p.caseExpr()
		}
	}
	return p.unexpected(prod, "an expression")
}

// parenExpr parses a group met in an expression position. What it holds is
// decided here, by its first token: a statement is a subquery, anything else is
// a parenthesised expression when it holds one item and a row constructor when
// it holds several. Both are written the same way, which is why the group needs
// no second spelling.
func (p *parser) parenExpr(g Group) error {
	if startsStatement(g) {
		return p.parens(g, (*parser).statement)
	}
	if len(compact(g.items)) == 0 {
		return p.parens(g, func(*parser) error { return nil })
	}
	return p.parens(g, (*parser).exprList)
}

// call implements
//
//	function_name ( [ ALL | DISTINCT ] expression [, ...] [ ORDER BY sort_key [, ...] ] )
//
// The parentheses come from Func, which is a constructor rather than a keyword:
// a function call is written with parentheses in SQL too.
func (p *parser) call(g Group) error {
	sub := &parser{toks: compact(g.items), e: p.e}
	p.e.open(g.name)
	if !sub.done() {
		if !sub.take(kw.ALL) {
			sub.take(kw.DISTINCT)
		}
		if err := sub.exprList(); err != nil {
			return err
		}
		// An aggregate may order its input, which is why this is not simply an
		// expression list.
		if sub.take(kw.ORDER) {
			if err := sub.want("ORDER BY", kw.BY); err != nil {
				return err
			}
			if err := sub.sortList(); err != nil {
				return err
			}
		}
	}
	if !sub.done() {
		return sub.unexpected("function call", "the end of the argument list")
	}
	p.e.close()
	return nil
}

// caseExpr implements
//
//	CASE [ expression ]
//	    WHEN expression THEN expression [ ... ]
//	    [ ELSE expression ]
//	END
//
// It needs no rule of its own to sit inside a comma-separated list: it is one
// expression, and it ends at END.
func (p *parser) caseExpr() error {
	p.take(kw.CASE)
	if !p.at(kw.WHEN) {
		if err := p.expr("CASE"); err != nil {
			return err
		}
	}
	if !p.at(kw.WHEN) {
		return p.unexpected("CASE", "keyword WHEN")
	}
	for p.take(kw.WHEN) {
		if err := p.expr("CASE"); err != nil {
			return err
		}
		if err := p.want("CASE", kw.THEN); err != nil {
			return err
		}
		if err := p.expr("CASE"); err != nil {
			return err
		}
	}
	if p.take(kw.ELSE) {
		if err := p.expr("CASE"); err != nil {
			return err
		}
	}
	return p.want("CASE", kw.END)
}

// typeName parses the right-hand side of a typecast. A simple name is written
// with Id, which covers jsonb, int and text; anything with a modifier or more
// than one word is written with Raw for now.
func (p *parser) typeName(prod string) error {
	switch t := p.peek().(type) {
	case Id:
		p.pos++
		p.e.word(string(t))
		return nil
	case rawFrag:
		p.pos++
		if t.err != nil {
			return t.err
		}
		p.e.rawFragment(t.parts, t.vals)
		return nil
	}
	return p.unexpected(prod, "a type name written with sb.Id or sb.Raw")
}

// similarTo implements
//
//	expression [ NOT ] SIMILAR TO expression
func (p *parser) similarTo(prod string) error {
	p.take(kw.SIMILAR)
	if err := p.want(prod, kw.TO); err != nil {
		return err
	}
	return p.operand(prod)
}
