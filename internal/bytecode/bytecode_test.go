package bytecode

import "testing"

func TestOpString(t *testing.T) {
	if OpAdd.String() != "add" {
		t.Errorf("OpAdd.String()=%q", OpAdd.String())
	}
	if OpReturn.String() != "return" {
		t.Errorf("OpReturn.String()=%q", OpReturn.String())
	}
	if Op(254).String() != "op?" {
		t.Errorf("unknown Op.String()=%q", Op(254).String())
	}
}
