package peer

import "testing"

func TestDialogUsesASCIILowerForNonUnicodeCaseFolding(t *testing.T) {
	// Mutation captured: regexp (?i) folds long s to ASCII s, unlike JavaScript /i without u.
	if !isDialogScreen("Trust this workspace", "", "idle") {
		t.Fatal("ASCII trust prompt was not recognized")
	}
	if isDialogScreen("truſt this workspace", "", "idle") {
		t.Fatal("long-s trust screen must not match JavaScript /i without u")
	}
}
