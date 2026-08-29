package sb_test

import (
	"testing"

	. "github.com/fujidaiti/psqlb/kw"
)

// A statement is a statement: the dispatcher accepts nothing else, and a group
// met where a statement belongs is parsed as one.
func TestStatementShape(t *testing.T) {
	runErrs(t, []ecase{
		{
			name: "not a statement",
			stmt: stmt(FROM, Users),
			want: "statement: expected keyword SELECT, INSERT, UPDATE or DELETE",
		},
	})
}
