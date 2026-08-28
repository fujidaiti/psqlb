// Package tok holds the token types that the two public packages share: the
// keyword type, the operator type and the interface every token satisfies.
//
// It exists because both public packages must name the keyword type and
// neither can own it: kw types its constants with it, and sb switches on it
// while parsing and constructs one for "::", and sb imports kw. The types are
// here rather than in kw because kw is dot-imported, and a dot-import must
// bring SQL words into the user's file scope and nothing else. The one name a
// user can see, Token, is aliased out of here by sb.
//
// There is one keyword type. What a keyword means is decided by the grammar
// position it is parsed in, not by its Go type.
package tok

// Token is what every token in a statement satisfies. The method is exported
// only because the token types of package sb must implement it from outside
// this package; nothing calls it. A type declared elsewhere therefore satisfies
// the interface, but the parser switches on the concrete types it knows and
// reports anything else as an error, so the set of usable tokens is closed by
// the parser rather than by the compiler.
type Token interface{ SQLToken() }

// Keyword is a single SQL keyword word. Compound phrases are written as
// several constants in a row: GROUP, BY and IS, NOT, NULL. The constants
// themselves are in package kw.
type Keyword string

func (Keyword) SQLToken() {}

// Operator is an operator symbol. It is produced only by normalization in sb,
// from a string that satisfies PostgreSQL's lexical rule for an operator name,
// so there is no list of operators anywhere and an operator an extension
// defines needs nothing that a core one does not.
type Operator string

func (Operator) SQLToken() {}
