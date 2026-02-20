// In Go, unit tests live right next to the files they are testing.
// Any file ending in '_test.go' will be ignored by the regular 'go build'
// compiler, but will be compiled and executed when you run 'go test'.
//
// Because this test file is in the same package ('example'), it can
// test unexported (private) functions inside this package without issues.
package example

import "testing"

// TestHello is a simple unit test for the Hello function.
// Every test function must start with 'Test' and take a pointer to testing.T.
// To run this, you can type `go test ./...` in the terminal.
func TestHello(t *testing.T) {
	// Normally you would capture output and assert it matches your expectations.
	// This is a basic skeleton of what a test function looks like.
	Hello()

	// If something failed, you would call t.Errorf("Expected X, got Y") or t.Fail()
}
