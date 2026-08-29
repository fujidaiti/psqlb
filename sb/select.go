package sb

import "github.com/fujidaiti/psqlb/kw"

// The SELECT statement: the set operations that combine query terms, the clauses of
// one term, and the two lists that are not clauses of their own — the output list,
// which RETURNING shares, and the sort key list, which the window definition shares.

// query implements
//
//	select_term [ { UNION | INTERSECT | EXCEPT } [ ALL | DISTINCT ] select_term ]
//
// A term is either a SELECT written in place or a group holding one. The
// parenthesised form is what lets a term carry its own ORDER BY and LIMIT,
// exactly as in SQL, and it is written the same way as every other group.
func (p *parser) query() error {
	if err := p.queryTerm(); err != nil {
		return err
	}
	for {
		switch {
		case p.take(kw.UNION), p.take(kw.INTERSECT), p.take(kw.EXCEPT):
		default:
			return nil
		}
		if !p.take(kw.ALL) {
			p.take(kw.DISTINCT)
		}
		if err := p.queryTerm(); err != nil {
			return err
		}
	}
}

func (p *parser) queryTerm() error {
	if g, ok := p.peek().(Group); ok && g.name == "" && startsStatement(g) {
		p.pos++
		return p.parens(g, (*parser).query)
	}
	if p.at(kw.SELECT) {
		return p.selectStmt()
	}
	if p.take(kw.VALUES) {
		return p.valuesRows()
	}
	return p.unexpected("statement", "keyword SELECT, INSERT, UPDATE or DELETE")
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
		g, err := p.group("DISTINCT ON", "a parenthesised list of expressions", "DISTINCT, ON, sb.P(...)")
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
	if p.take(kw.GROUP) {
		if err := p.want("GROUP BY", kw.BY); err != nil {
			return err
		}
		if err := p.groupList(); err != nil {
			return err
		}
	}
	if p.take(kw.HAVING) {
		if err := p.expr("HAVING"); err != nil {
			return err
		}
	}
	if p.take(kw.WINDOW) {
		if err := p.windowList(); err != nil {
			return err
		}
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
	return nil
}

// groupList parses the GROUP BY list.
//
//	grouping_element [, ...]
//
// A grouping element is an expression, an empty group, or a call of ROLLUP,
// CUBE or GROUPING SETS, all of which are already expressions here.
func (p *parser) groupList() error { return p.commaExprs("GROUP BY") }

// targetList parses an output list: the one after SELECT and the one after
// RETURNING, which have the same grammar.
//
//	expression [ AS output_name ] [, ...]
//
// The comma is written by this production, which is the whole reason the
// package exists. An alias must be written with AS: an identifier standing on
// its own is the next item of the list, never an alias for the one before it.
func (p *parser) targetList(prod string) error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		// SELECT * and RETURNING *. No alias follows: "SELECT * AS x" is not
		// legal SQL.
		if p.atStar() {
			p.pos++
			p.e.word(string(star))
		} else {
			if err := p.expr(prod); err != nil {
				return err
			}
			if p.take(kw.AS) {
				if err := p.alias(prod); err != nil {
					return err
				}
			}
		}
		if !p.startsExpr() && !p.atStar() {
			return nil
		}
	}
}

func (p *parser) selectList() error { return p.targetList("SELECT") }

// returning parses the optional RETURNING clause the three write statements
// share.
func (p *parser) returning() error {
	if !p.take(kw.RETURNING) {
		return nil
	}
	return p.targetList("RETURNING")
}

func (p *parser) alias(prod string) error {
	id, ok := p.peek().(I)
	if !ok {
		return p.unexpected(prod, "an alias written with sb.I")
	}
	p.pos++
	p.e.word(string(id))
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
