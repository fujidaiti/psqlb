package sb

import "github.com/fujidaiti/psqlb/kw"

// The WITH clause: the common table expressions and the statement they are for.

// withStmt implements
//
//	WITH [ RECURSIVE ] with_query [, ...] statement
//
// The CTE bodies are parenthesised in SQL, so each is written with P like every
// other group, and the statement they are for follows them as plain tokens.
func (p *parser) withStmt() error {
	p.take(kw.WITH)
	p.take(kw.RECURSIVE)
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		if err := p.withQuery(); err != nil {
			return err
		}
		if _, ok := p.peek().(I); !ok {
			break
		}
	}
	if p.at(kw.WITH) {
		return p.unexpected("WITH", "the statement the queries are for")
	}
	return p.statement()
}

// withQuery implements
//
//	name [ ( column_name [, ...] ) ] AS [ [ NOT ] MATERIALIZED ] ( query )
func (p *parser) withQuery() error {
	name, ok := p.peek().(I)
	if !ok {
		return p.unexpected("WITH", "a query name written with sb.I")
	}
	p.pos++
	p.e.word(string(name))

	if g, ok := p.peek().(Group); ok && g.name == "" && !startsStatement(g) {
		p.pos++
		if err := p.parens(g, (*parser).nameList); err != nil {
			return err
		}
	}
	if err := p.want("WITH", kw.AS); err != nil {
		return err
	}
	if p.take(kw.NOT) {
		if err := p.want("WITH", kw.MATERIALIZED); err != nil {
			return err
		}
	} else {
		p.take(kw.MATERIALIZED)
	}

	g, err := p.group("WITH", "a parenthesised query as the body", "AS, sb.P(SELECT, ...)")
	if err != nil {
		return err
	}
	return p.parens(g, (*parser).statement)
}
