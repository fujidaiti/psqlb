// Package psqlb holds the SQL keywords and operators of the psqlb DSL, and
// nothing else. It is meant to be dot-imported, so that a statement reads like
// the SQL it produces:
//
//	import (
//		. "github.com/fujidaiti/psqlb"
//		"github.com/fujidaiti/psqlb/sb"
//	)
//
//	sb.ToSQL(SELECT, UsersID, FROM, Users, WHERE, UsersAge, GTE, sb.V(18))
//
// Everything that is not a SQL keyword — ToSQL, P, F, I, V, RawExpr, RawOp —
// lives in package sb and is written with that prefix, so that dot-importing
// this package brings only SQL vocabulary into scope. The prefix is also what
// lets those names be one letter. The design document is the package doc
// comment of sb.
//
// Every keyword is one word. A phrase is written as the words it is made of:
// GROUP, BY and IS, NOT, NULL and LEFT, OUTER, JOIN. The parser knows where
// each one may appear, so a keyword needs no Go type, no second spelling for a
// second role, and no parentheses of its own.
package psqlb

import (
	"github.com/fujidaiti/psqlb/internal/kw"
)

// ===========================================================================
// Keywords
// ===========================================================================
//
// The constants are declared in internal/kw and re-exported here, so that the
// spelling a user writes and the value the parser compares against are the same
// thing. A keyword the parser does not know cannot be written, since kw is
// internal.

// Statements and clauses.
const (
	SELECT    = kw.SELECT
	FROM      = kw.FROM
	WHERE     = kw.WHERE
	GROUP     = kw.GROUP
	BY        = kw.BY
	HAVING    = kw.HAVING
	WINDOW    = kw.WINDOW
	ORDER     = kw.ORDER
	LIMIT     = kw.LIMIT
	OFFSET    = kw.OFFSET
	INSERT    = kw.INSERT
	INTO      = kw.INTO
	VALUES    = kw.VALUES
	UPDATE    = kw.UPDATE
	SET       = kw.SET
	DELETE    = kw.DELETE
	USING     = kw.USING
	RETURNING = kw.RETURNING
	CONFLICT  = kw.CONFLICT
	DO        = kw.DO
	NOTHING   = kw.NOTHING
	WITH      = kw.WITH
	RECURSIVE = kw.RECURSIVE
)

// Modifiers of the SELECT list and of a sort key.
const (
	ALL      = kw.ALL
	DISTINCT = kw.DISTINCT
	AS       = kw.AS
	ASC      = kw.ASC
	DESC     = kw.DESC
	NULLS    = kw.NULLS
	FIRST    = kw.FIRST
	LAST     = kw.LAST
)

// Joins and set operations.
const (
	JOIN      = kw.JOIN
	LEFT      = kw.LEFT
	RIGHT     = kw.RIGHT
	FULL      = kw.FULL
	INNER     = kw.INNER
	OUTER     = kw.OUTER
	CROSS     = kw.CROSS
	NATURAL   = kw.NATURAL
	LATERAL   = kw.LATERAL
	ON        = kw.ON
	UNION     = kw.UNION
	INTERSECT = kw.INTERSECT
	EXCEPT    = kw.EXCEPT
)

// Expressions. A keyword that SQL always follows with a parenthesised group —
// IN, EXISTS, ANY, FILTER, OVER — is a constant like any other, and the group
// after it is written with sb.P. The parser requires it to be there.
const (
	AND       = kw.AND
	OR        = kw.OR
	NOT       = kw.NOT
	IS        = kw.IS
	NULL      = kw.NULL
	TRUE      = kw.TRUE
	FALSE     = kw.FALSE
	UNKNOWN   = kw.UNKNOWN
	IN        = kw.IN
	BETWEEN   = kw.BETWEEN
	EXISTS    = kw.EXISTS
	ANY       = kw.ANY
	SOME      = kw.SOME
	LIKE      = kw.LIKE
	ILIKE     = kw.ILIKE
	SIMILAR   = kw.SIMILAR
	TO        = kw.TO
	COLLATE   = kw.COLLATE
	CASE      = kw.CASE
	WHEN      = kw.WHEN
	THEN      = kw.THEN
	ELSE      = kw.ELSE
	END       = kw.END
	DEFAULT   = kw.DEFAULT
	FILTER    = kw.FILTER
	OVER      = kw.OVER
	PARTITION = kw.PARTITION
)

// Window frames.
const (
	ROW       = kw.ROW
	ROWS      = kw.ROWS
	RANGE     = kw.RANGE
	GROUPS    = kw.GROUPS
	UNBOUNDED = kw.UNBOUNDED
	PRECEDING = kw.PRECEDING
	FOLLOWING = kw.FOLLOWING
	CURRENT   = kw.CURRENT
)

// MATERIALIZED qualifies a CTE body.
const MATERIALIZED = kw.MATERIALIZED

// STAR is "*", the whole row. It is an expression on its own, in a SELECT list
// and as the argument of a function.
//
//	SELECT, STAR, FROM, Users        // SELECT * FROM users
//	sb.F("COUNT", STAR)           // COUNT(*)
const STAR = kw.STAR

// TYPECAST is "::". The name after it is a type name rather than an
// expression, and both are emitted with no spaces around the operator.
//
//	UsersMeta, TYPECAST, sb.I("jsonb") // users.meta::jsonb
//
// It is spelled TYPECAST rather than CAST because PostgreSQL calls "::" a
// typecast and reserves CAST for the CAST(x AS type) form, which is a different
// syntax.
const TYPECAST = kw.TYPECAST

// ===========================================================================
// Operators
// ===========================================================================

// The operators that have a name here. Any other is written with sb.RawOp, since
// operators are not a fixed list: extensions add their own.
//
// TODO: EQ, NE, GTE and the rest are not SQL spellings, which is a deviation
// from the first golden rule. Go identifiers cannot be "=" or ">=", so the
// alternative is sb.RawOp(">=") everywhere.
const (
	EQ  kw.Operator = "="
	NE  kw.Operator = "<>"
	GT  kw.Operator = ">"
	GTE kw.Operator = ">="
	LT  kw.Operator = "<"
	LTE kw.Operator = "<="
)
