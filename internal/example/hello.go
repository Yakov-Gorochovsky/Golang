// Package example demonstrates the use of the 'internal' directory.
//
// In Go, the 'internal' directory is special. Code inside an 'internal'
// directory can only be imported by packages rooted in the parent of the 'internal' directory.
// For example, this package can be imported by cmd/server/ or other packages inside internal/,
// but if you published this project, other developers importing your project
// would NOT be able to import this 'example' package.
//
// It is used to encapsulate your core business logic and prevent external projects
// from depending on your internal implementation details.
package example

import "fmt"

// Hello is a sample function that can be called from main.go
func Hello() {
	fmt.Println("Hello from the internal package!")
}
