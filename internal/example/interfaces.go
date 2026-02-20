// Package example demonstrates how Go compares to Java.
package example

import "fmt"

// 1. NO CLASSES, ONLY STRUCTS
// Go doesn't have the concept of a "class" or "objects" in the traditional OOP sense.
// Instead, it has "structs" which hold state (data).
type Dog struct {
	Name  string
	Breed string
}

// You can attach "methods" to a struct using a "receiver" (the `(d Dog)` part).
// This acts similar to a class method in Java.
func (d Dog) Speak() string {
	return "Woof! My name is " + d.Name
}

// 2. INTERFACES ARE IMPLICIT
// Yes, Go has interfaces! But unlike Java where you must write `class Dog implements Animal`,
// Go uses "Implicit Interfaces" (also known as duck typing).
type Animal interface {
	Speak() string
}

// Because the `Dog` struct above has a `Speak() string` method, it IMPLICITLY
// implements the `Animal` interface. The compiler figures this out automatically!
// You never explicitly declare "implements".

// MakeNoise accepts any type that satisfies the Animal interface.
func MakeNoise(a Animal) {
	fmt.Println("The animal says:", a.Speak())
}

// Example usage:
// d := Dog{Name: "Buddy"}
// MakeNoise(d) // This works perfectly!
