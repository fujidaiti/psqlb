// Package kw holds the token types that the two public packages share: the
// keyword type and every keyword constant, and the operator type.
//
// It exists so that psqlb can declare the keyword constants and sb can compare
// the tokens it parses against the same values. The names are exported because
// both packages need them, and the package is internal so that they are not
// public API: a user cannot declare a keyword of their own, which matters more
// under a parser than it did under a renderer, because a keyword the parser
// does not know is a keyword the parser cannot place.
//
// There is one keyword type. What a keyword means is decided by the grammar
// position it is parsed in, not by its Go type.
package kw

// Token is what every token in a statement satisfies. The method is exported
// only because the token types of package sb must implement it from outside
// this package; nothing calls it. A type declared outside these two packages
// can satisfy it, but the parser switches on the concrete types it knows and
// reports anything else as an error, so the set of usable tokens is closed.
type Token interface{ SQLToken() }

// Keyword is a single SQL keyword word. Compound phrases are written as
// several constants in a row: GROUP, BY and IS, NOT, NULL.
type Keyword string

func (Keyword) SQLToken() {}

// Operator is an operator symbol written by hand, which is what sb.Op
// produces. Operators are not a fixed list, since extensions add their own, so
// they are not enumerated here.
type Operator string

func (Operator) SQLToken() {}

// The keyword vocabulary. Every one is a single word, so that a token list is
// the SQL text word for word. Not all of them are parsed yet; one outside the
// supported subset is reported as "not supported yet".
const (
	// Statements and clauses.
	SELECT    Keyword = "SELECT"
	FROM      Keyword = "FROM"
	WHERE     Keyword = "WHERE"
	GROUP     Keyword = "GROUP"
	BY        Keyword = "BY"
	HAVING    Keyword = "HAVING"
	WINDOW    Keyword = "WINDOW"
	ORDER     Keyword = "ORDER"
	LIMIT     Keyword = "LIMIT"
	OFFSET    Keyword = "OFFSET"
	INSERT    Keyword = "INSERT"
	INTO      Keyword = "INTO"
	VALUES    Keyword = "VALUES"
	UPDATE    Keyword = "UPDATE"
	SET       Keyword = "SET"
	DELETE    Keyword = "DELETE"
	USING     Keyword = "USING"
	RETURNING Keyword = "RETURNING"
	CONFLICT  Keyword = "CONFLICT"
	DO        Keyword = "DO"
	NOTHING   Keyword = "NOTHING"
	WITH      Keyword = "WITH"
	RECURSIVE Keyword = "RECURSIVE"

	// Modifiers of the SELECT list and of a sort key.
	ALL      Keyword = "ALL"
	DISTINCT Keyword = "DISTINCT"
	AS       Keyword = "AS"
	ASC      Keyword = "ASC"
	DESC     Keyword = "DESC"
	NULLS    Keyword = "NULLS"
	FIRST    Keyword = "FIRST"
	LAST     Keyword = "LAST"

	// Joins and set operations.
	JOIN      Keyword = "JOIN"
	LEFT      Keyword = "LEFT"
	RIGHT     Keyword = "RIGHT"
	FULL      Keyword = "FULL"
	INNER     Keyword = "INNER"
	OUTER     Keyword = "OUTER"
	CROSS     Keyword = "CROSS"
	NATURAL   Keyword = "NATURAL"
	LATERAL   Keyword = "LATERAL"
	ON        Keyword = "ON"
	UNION     Keyword = "UNION"
	INTERSECT Keyword = "INTERSECT"
	EXCEPT    Keyword = "EXCEPT"

	// Expressions.
	AND       Keyword = "AND"
	OR        Keyword = "OR"
	NOT       Keyword = "NOT"
	IS        Keyword = "IS"
	NULL      Keyword = "NULL"
	TRUE      Keyword = "TRUE"
	FALSE     Keyword = "FALSE"
	UNKNOWN   Keyword = "UNKNOWN"
	IN        Keyword = "IN"
	BETWEEN   Keyword = "BETWEEN"
	EXISTS    Keyword = "EXISTS"
	ANY       Keyword = "ANY"
	SOME      Keyword = "SOME"
	LIKE      Keyword = "LIKE"
	ILIKE     Keyword = "ILIKE"
	SIMILAR   Keyword = "SIMILAR"
	TO        Keyword = "TO"
	COLLATE   Keyword = "COLLATE"
	CASE      Keyword = "CASE"
	WHEN      Keyword = "WHEN"
	THEN      Keyword = "THEN"
	ELSE      Keyword = "ELSE"
	END       Keyword = "END"
	DEFAULT   Keyword = "DEFAULT"
	FILTER    Keyword = "FILTER"
	OVER      Keyword = "OVER"
	PARTITION Keyword = "PARTITION"

	// Window frames.
	ROW       Keyword = "ROW"
	ROWS      Keyword = "ROWS"
	RANGE     Keyword = "RANGE"
	GROUPS    Keyword = "GROUPS"
	UNBOUNDED Keyword = "UNBOUNDED"
	PRECEDING Keyword = "PRECEDING"
	FOLLOWING Keyword = "FOLLOWING"
	CURRENT   Keyword = "CURRENT"

	// Two tokens that are not words. They are keywords because the parser
	// recognises them by value at particular grammar positions: STAR is an
	// expression on its own, and TYPECAST is followed by a type name rather
	// than by an expression.
	STAR     Keyword = "*"
	TYPECAST Keyword = "::"

	// MATERIALIZED qualifies a CTE body.
	MATERIALIZED Keyword = "MATERIALIZED"
)
