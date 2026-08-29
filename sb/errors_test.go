package sb_test

import (
	"errors"
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
	"github.com/fujidaiti/psqlb/sb"
)

// The three error types are distinct so that a caller, and a test, can tell them
// apart without reading the message. Which sequences produce which is covered by
// the test file of the production that rejects them; this is only the property
// that the types themselves are told apart.
func TestErrorTypes(t *testing.T) {
	var syntax *sb.SyntaxError
	_, _, err := sb.ToSQL(FROM, Users)
	if !errors.As(err, &syntax) {
		t.Errorf("want *sb.SyntaxError, got %T: %v", err, err)
	} else if syntax.Pos != 0 || syntax.Production != "statement" {
		t.Errorf("unexpected fields: %#v", syntax)
	}

	var missing *sb.MissingError
	_, _, err = sb.ToSQL(SELECT, "*", FROM, sb.P(SELECT, UsersID, FROM, Users))
	if !errors.As(err, &missing) {
		t.Errorf("want *sb.MissingError, got %T: %v", err, err)
	}
}
