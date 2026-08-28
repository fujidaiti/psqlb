package sb

import (
	"fmt"
	"strings"
)

// emitter is the output side of the parser. Every separator in the generated
// SQL comes from here, and every separator is chosen by the grammar production
// that calls it, so no rule about separators is spread across the tokens.
//
// sep is the text to write before the next fragment. It is a space by default
// and empty immediately after an opening parenthesis, which is what keeps
// "(users.id" from becoming "( users.id".
type emitter struct {
	b    strings.Builder
	args []any
	sep  string
}

// word writes a fragment separated from the one before it.
func (e *emitter) word(s string) {
	if s == "" {
		return
	}
	e.b.WriteString(e.sep)
	e.b.WriteString(s)
	e.sep = " "
}

// glue writes a fragment with no separator before it: the comma that ends a
// list item, a closing parenthesis, and the two halves of a typecast.
func (e *emitter) glue(s string) {
	e.b.WriteString(s)
	e.sep = " "
}

// comma ends a list item. The space before the next item comes from sep, so
// the result is "a, b".
func (e *emitter) comma() { e.glue(",") }

// open writes an opening parenthesis, preceded by name for a function call.
// The next fragment is written with no separator.
func (e *emitter) open(name string) {
	e.word(name + "(")
	e.sep = ""
}

func (e *emitter) close() { e.glue(")") }

// bind appends a value and writes its placeholder. Numbering is emission
// order, which is token order, so a nested statement numbers correctly at any
// depth without a counter or a renumbering pass.
func (e *emitter) bind(v any) {
	e.args = append(e.args, v)
	e.word(fmt.Sprintf("$%d", len(e.args)))
}

// rawFragment writes a hand-written fragment, binding its values at the
// "$0" markers as it goes.
func (e *emitter) rawFragment(parts []string, vals []any) {
	var b strings.Builder
	b.WriteString(parts[0])
	for i, part := range parts[1:] {
		e.args = append(e.args, vals[i])
		fmt.Fprintf(&b, "$%d", len(e.args))
		b.WriteString(part)
	}
	e.word(b.String())
}

// noSpace suppresses the separator before the next fragment, so that it is
// written glued to the one before it.
func (e *emitter) noSpace() { e.sep = "" }
