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

func (p *parser) unexpected(prod, want string) error {
	return &SyntaxError{Production: prod, Want: want, Got: describe(p.peek()), Pos: p.pos}
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
		if err := p.expr(prod); err != nil {
			return err
		}
		if p.take(kw.AS) {
			if err := p.alias(prod); err != nil {
				return err
			}
		}
		if !p.startsExpr() {
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
