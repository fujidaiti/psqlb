package sb

import "github.com/fujidaiti/psqlb/kw"

// The FROM clause: the list, the joins that chain within one item, and the item
// itself — a table, a subquery or a function call.

// fromList parses the FROM list.
//
//	from_item [, ...]
func (p *parser) fromList() error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		if err := p.joinedTable(); err != nil {
			return err
		}
		if !p.startsFromItem() {
			return nil
		}
	}
}

// startsFromItem reports whether the next token can begin a FROM item. A value
// or a hand-written fragment cannot: a FROM item is a table, a subquery or a
// function call, and nothing else.
func (p *parser) startsFromItem() bool {
	switch p.peek().(type) {
	case I, Group:
		return true
	}
	return p.at(kw.LATERAL)
}

// joinedTable implements
//
//	from_item [ NATURAL ] join_type from_item { ON condition | USING ( column [, ...] ) }
//
// Joins associate to the left and chain, so this is a loop rather than
// recursion. Parentheses around a join are written with P like any others.
func (p *parser) joinedTable() error {
	if err := p.fromItem(); err != nil {
		return err
	}
	for {
		joined, err := p.joinClause()
		if err != nil {
			return err
		}
		if !joined {
			return nil
		}
	}
}

// joinClause parses one join and reports whether there was one.
//
//	join_type: [ INNER ] JOIN | { LEFT | RIGHT | FULL } [ OUTER ] JOIN | CROSS JOIN
//
// A CROSS or NATURAL join takes no condition and every other join requires one,
// which PostgreSQL enforces and so does this.
func (p *parser) joinClause() (bool, error) {
	switch {
	case p.at(kw.JOIN), p.at(kw.NATURAL), p.at(kw.CROSS),
		p.at(kw.INNER), p.at(kw.LEFT), p.at(kw.RIGHT), p.at(kw.FULL):
	default:
		return false, nil
	}

	natural := p.take(kw.NATURAL)
	cross := p.take(kw.CROSS)
	if !cross && !p.take(kw.INNER) {
		if p.take(kw.LEFT) || p.take(kw.RIGHT) || p.take(kw.FULL) {
			p.take(kw.OUTER)
		}
	}
	if err := p.want("JOIN", kw.JOIN); err != nil {
		return false, err
	}
	if err := p.fromItem(); err != nil {
		return false, err
	}

	switch {
	case cross || natural:
		if p.at(kw.ON) || p.at(kw.USING) {
			return false, p.unexpected("JOIN", "no condition, since a CROSS or NATURAL join takes none")
		}
	case p.take(kw.ON):
		if err := p.expr("JOIN ON"); err != nil {
			return false, err
		}
	case p.take(kw.USING):
		g, err := p.group("JOIN USING", "a parenthesised list of column names", "USING, sb.P(...)")
		if err != nil {
			return false, err
		}
		if err := p.parens(g, (*parser).nameList); err != nil {
			return false, err
		}
	default:
		return false, &MissingError{
			Production: "JOIN",
			Want:       "a join condition",
			Fix:        "ON, <condition> or USING, sb.P(...)",
		}
	}
	return true, nil
}

// fromItem implements, of the from_item synopsis:
//
//	table_name [ AS alias ]
//	[ LATERAL ] ( select ) AS alias
//	[ LATERAL ] function_name ( argument [, ...] ) [ AS alias ]
//
// The alias on a subquery is required by PostgreSQL, so its absence is reported
// here rather than by the server. That is the one requirement of this kind the
// grammar can state: whether a name in a list is an alias or the next item
// cannot be known, so an alias is always written with AS.
func (p *parser) fromItem() error {
	// LATERAL needs no syntax of its own: it is a keyword in front of the item
	// it applies to, which is a subquery or a function call.
	lateral := p.take(kw.LATERAL)

	subquery := false
	switch t := p.peek().(type) {
	case I:
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
	if lateral && !subquery {
		if _, ok := p.toks[p.pos-1].(Group); !ok {
			return p.unexpected("FROM", "a subquery or a function call after LATERAL")
		}
	}

	if p.take(kw.AS) {
		return p.alias("FROM")
	}
	if subquery {
		return &MissingError{
			Production: "FROM",
			Want:       "an alias on a subquery",
			Fix:        `sb.P(...), AS, sb.I("t")`,
		}
	}
	return nil
}
