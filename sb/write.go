package sb

import "github.com/fujidaiti/psqlb/internal/kw"

// The three write statements. Each one is written as a flat token list in the
// order SQL reads it, and each clause below is the synopsis from the
// PostgreSQL reference page for that statement.

// insertStmt implements
//
//	INSERT INTO table_name [ AS alias ] [ ( column_name [, ...] ) ]
//	    { VALUES ( expression [, ...] ) [, ...] | query }
//	    [ ON CONFLICT [ ( column_name [, ...] ) ] DO NOTHING
//	                                             | DO UPDATE SET ... [ WHERE condition ] ]
//	    [ RETURNING output_expression [ AS name ] [, ...] ]
func (p *parser) insertStmt() error {
	p.take(kw.INSERT)
	if err := p.want("INSERT", kw.INTO); err != nil {
		return err
	}
	if err := p.table("INSERT"); err != nil {
		return err
	}

	// A group here is the column list. It cannot be anything else at this
	// position, except a parenthesised query, which is what its first token
	// says.
	if g, ok := p.peek().(Group); ok && g.name == "" && !startsStatement(g) {
		p.pos++
		if err := p.parens(g, (*parser).nameList); err != nil {
			return err
		}
	}

	switch {
	case p.take(kw.VALUES):
		if err := p.valuesRows(); err != nil {
			return err
		}
	case p.at(kw.SELECT), p.at(kw.WITH):
		// INSERT ... SELECT. The query is not parenthesised, so it is parsed
		// in place rather than as a group.
		if err := p.statement(); err != nil {
			return err
		}
	default:
		return p.unexpected("INSERT", "keyword VALUES or a query")
	}

	if p.take(kw.ON) {
		if err := p.onConflict(); err != nil {
			return err
		}
	}
	return p.returning()
}

// valuesRows parses the rows of a VALUES clause. Each row is parenthesised in
// SQL, so each is written with S and the comma between them comes from here.
//
//	( expression [, ...] ) [, ...]
func (p *parser) valuesRows() error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		g, err := p.group("VALUES", "a parenthesised row after VALUES", "VALUES, sb.S(...)")
		if err != nil {
			return err
		}
		if err := p.parens(g, (*parser).exprList); err != nil {
			return err
		}
		if _, ok := p.peek().(Group); !ok {
			return nil
		}
	}
}

// onConflict implements the conflict clause of INSERT. The ON has already been
// consumed, since it is what brought the parser here.
//
//	ON CONFLICT [ ( column_name [, ...] ) ] DO NOTHING | DO UPDATE SET ... [ WHERE condition ]
func (p *parser) onConflict() error {
	if err := p.want("ON CONFLICT", kw.CONFLICT); err != nil {
		return err
	}
	if g, ok := p.peek().(Group); ok && g.name == "" {
		p.pos++
		if err := p.parens(g, (*parser).nameList); err != nil {
			return err
		}
	}
	if !p.take(kw.DO) {
		if p.at(kw.ON) {
			return &UnsupportedError{"ON CONFLICT ON CONSTRAINT"}
		}
		return p.unexpected("ON CONFLICT", "keyword DO")
	}
	if p.take(kw.NOTHING) {
		return nil
	}
	if !p.take(kw.UPDATE) {
		return p.unexpected("ON CONFLICT", "keyword NOTHING or UPDATE")
	}
	if err := p.want("DO UPDATE", kw.SET); err != nil {
		return err
	}
	if err := p.setList(); err != nil {
		return err
	}
	if p.take(kw.WHERE) {
		return p.expr("WHERE")
	}
	return nil
}

// updateStmt implements
//
//	UPDATE table_name [ AS alias ] SET assignment [, ...]
//	    [ FROM from_item [, ...] ]
//	    [ WHERE condition ]
//	    [ RETURNING output_expression [ AS name ] [, ...] ]
func (p *parser) updateStmt() error {
	p.take(kw.UPDATE)
	if err := p.table("UPDATE"); err != nil {
		return err
	}
	if err := p.want("UPDATE", kw.SET); err != nil {
		return err
	}
	if err := p.setList(); err != nil {
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
	return p.returning()
}

// deleteStmt implements
//
//	DELETE FROM table_name [ AS alias ]
//	    [ USING from_item [, ...] ]
//	    [ WHERE condition ]
//	    [ RETURNING output_expression [ AS name ] [, ...] ]
func (p *parser) deleteStmt() error {
	p.take(kw.DELETE)
	if err := p.want("DELETE", kw.FROM); err != nil {
		return err
	}
	if err := p.table("DELETE"); err != nil {
		return err
	}
	if p.take(kw.USING) {
		if err := p.fromList(); err != nil {
			return err
		}
	}
	if p.take(kw.WHERE) {
		if err := p.expr("WHERE"); err != nil {
			return err
		}
	}
	return p.returning()
}

// table parses the target of a write statement.
//
//	table_name [ AS alias ]
func (p *parser) table(prod string) error {
	name, ok := p.peek().(Id)
	if !ok {
		return p.unexpected(prod, "a table name written with sb.Id")
	}
	p.pos++
	p.e.word(string(name))
	if p.take(kw.AS) {
		return p.alias(prod)
	}
	return nil
}

// setList parses the assignments of SET.
//
//	{ column_name = expression | ( column_name [, ...] ) = ( expression [, ...] ) } [, ...]
//
// The table qualifier is stripped from the name on the left of the "=", since
// "SET users.status = ..." is not legal. This is the one deliberate exception
// of its kind left in the package: SQL has no such rule, and the DSL text
// differs from the SQL text. It is kept because writing sb.Id("status") for
// every assignment is the common case and the qualified column constant is the
// one already at hand. It applies at this grammar position and nowhere else, so
// the expression on the right of the "=" keeps its qualifiers, and so does the
// column list of INSERT.
//
// TODO: this remains a rule SQL does not have. Either find a spelling that
// needs no rewriting, or record it as permanent.
func (p *parser) setList() error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		if err := p.assignment(); err != nil {
			return err
		}
		if !p.startsExpr() {
			return nil
		}
	}
}

func (p *parser) assignment() error {
	switch t := p.peek().(type) {
	case Id:
		p.pos++
		p.e.word(unqualify(t))
	case Group:
		if t.name != "" {
			return p.unexpected("SET", "a column name written with sb.Id")
		}
		p.pos++
		if err := p.parens(t, (*parser).columnList); err != nil {
			return err
		}
	default:
		return p.unexpected("SET", "a column name written with sb.Id")
	}

	op, ok := p.peek().(kw.Operator)
	if !ok || op != "=" {
		return p.unexpected("SET", `operator "=", written EQ`)
	}
	p.pos++
	p.e.word("=")
	return p.expr("SET")
}

// columnList parses the target names of a multi-column assignment. They are
// unqualified for the same reason the single-column form is.
func (p *parser) columnList() error {
	return p.names("SET", unqualify)
}

// nameList parses a list of column names written as they stand: the column list
// of INSERT and the conflict target of ON CONFLICT. PostgreSQL forbids a
// qualifier at both positions, but nothing is rewritten here, because the
// stripping in SET is a convenience rather than a rule this package wants to
// spread.
func (p *parser) nameList() error {
	return p.names("column list", func(i Id) string { return string(i) })
}

func (p *parser) names(prod string, text func(Id) string) error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		name, ok := p.peek().(Id)
		if !ok {
			return p.unexpected(prod, "a column name written with sb.Id")
		}
		p.pos++
		p.e.word(text(name))
		if p.done() {
			return nil
		}
	}
}
