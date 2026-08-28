package sb

import "fmt"

// The package reports three classes of error and never panics. They are
// distinct types so that a caller, and a test, can tell "I wrote this wrong"
// from "this package has not got there yet".

// SyntaxError says that the token at Pos is not legal at this point in the
// grammar. Pos is the index within the group the token was written in, since a
// group is parsed on its own.
type SyntaxError struct {
	Production string // the grammar production being parsed
	Want       string // what would have been legal here
	Got        string // what was found instead
	Pos        int    // index of the offending token within its group
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("psqlb: %s: expected %s, got %s (token %d)", e.Production, e.Want, e.Got, e.Pos)
}

// MissingError says that a position requires something that is not there: a
// group where SQL always parenthesises, or the alias that PostgreSQL requires
// on a subquery in FROM. Fix names the way to write it.
type MissingError struct {
	Production string
	Want       string
	Fix        string
}

func (e *MissingError) Error() string {
	s := fmt.Sprintf("psqlb: %s: %s is required", e.Production, e.Want)
	if e.Fix != "" {
		s += "; write " + e.Fix
	}
	return s
}

// UnsupportedError says that the construct is legal PostgreSQL but outside the
// modelled subset. Coverage grows in phases, and incompleteness is a normal
// state for this package: it is reported honestly rather than rendered anyway.
type UnsupportedError struct {
	Construct string
}

func (e *UnsupportedError) Error() string {
	return "psqlb: not supported yet: " + e.Construct
}
