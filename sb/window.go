package sb

import "github.com/fujidaiti/psqlb/internal/kw"

// Window functions, and the two things that attach to an aggregate call.
//
// This is where the old engine's second unsolved problem was: OVER accepts a
// window name or a parenthesised window definition, and no token kind expresses
// "either of these". A production can, because it is reached only where a
// window specification belongs and can look at what is actually written.

// filter implements
//
//	aggregate_call FILTER ( WHERE condition )
func (p *parser) filter(prod string) error {
	p.take(kw.FILTER)
	g, err := p.group(prod, "a parenthesised WHERE clause after FILTER", "FILTER, sb.S(WHERE, ...)")
	if err != nil {
		return err
	}
	return p.parens(g, func(sub *parser) error {
		if err := sub.want("FILTER", kw.WHERE); err != nil {
			return err
		}
		return sub.expr("FILTER")
	})
}

// over implements
//
//	function_call OVER window_name
//	function_call OVER ( window_definition )
func (p *parser) over(prod string) error {
	p.take(kw.OVER)
	switch t := p.peek().(type) {
	case Id:
		p.pos++
		p.e.word(string(t))
		return nil
	case Group:
		if t.name == "" {
			p.pos++
			return p.parens(t, (*parser).windowDef)
		}
	}
	return p.unexpected(prod, "a window name or a parenthesised window definition")
}

// windowList parses the WINDOW clause of SELECT.
//
//	window_name AS ( window_definition ) [, ...]
func (p *parser) windowList() error {
	for i := 0; ; i++ {
		if i > 0 {
			p.e.comma()
		}
		name, ok := p.peek().(Id)
		if !ok {
			return p.unexpected("WINDOW", "a window name written with sb.Id")
		}
		p.pos++
		p.e.word(string(name))
		if err := p.want("WINDOW", kw.AS); err != nil {
			return err
		}
		g, err := p.group("WINDOW", "a parenthesised window definition", "AS, sb.S(...)")
		if err != nil {
			return err
		}
		if err := p.parens(g, (*parser).windowDef); err != nil {
			return err
		}
		if _, ok := p.peek().(Id); !ok {
			return nil
		}
	}
}

// windowDef implements
//
//	[ existing_window_name ]
//	[ PARTITION BY expression [, ...] ]
//	[ ORDER BY sort_key [, ...] ]
//	[ frame_clause ]
//
// Every part is optional, so an empty group is a valid window definition.
func (p *parser) windowDef() error {
	if name, ok := p.peek().(Id); ok {
		p.pos++
		p.e.word(string(name))
	}
	if p.take(kw.PARTITION) {
		if err := p.want("PARTITION BY", kw.BY); err != nil {
			return err
		}
		if err := p.commaExprs("PARTITION BY"); err != nil {
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
	return p.frameClause()
}

// frameClause implements
//
//	{ RANGE | ROWS | GROUPS } frame_start
//	{ RANGE | ROWS | GROUPS } BETWEEN frame_start AND frame_end
//
// The AND belongs to BETWEEN here as it does in the predicate, and is consumed
// by this production rather than read as the boolean operator.
func (p *parser) frameClause() error {
	if !p.take(kw.RANGE) && !p.take(kw.ROWS) && !p.take(kw.GROUPS) {
		return nil
	}
	if !p.take(kw.BETWEEN) {
		return p.frameBound()
	}
	if err := p.frameBound(); err != nil {
		return err
	}
	if err := p.want("frame clause", kw.AND); err != nil {
		return err
	}
	return p.frameBound()
}

// frameBound implements
//
//	UNBOUNDED PRECEDING | offset PRECEDING | CURRENT ROW
//	                    | offset FOLLOWING | UNBOUNDED FOLLOWING
func (p *parser) frameBound() error {
	switch {
	case p.take(kw.CURRENT):
		return p.want("frame clause", kw.ROW)
	case p.take(kw.UNBOUNDED):
	default:
		if err := p.operand("frame clause"); err != nil {
			return err
		}
	}
	if !p.take(kw.PRECEDING) && !p.take(kw.FOLLOWING) {
		return p.unexpected("frame clause", "keyword PRECEDING or FOLLOWING")
	}
	return nil
}
