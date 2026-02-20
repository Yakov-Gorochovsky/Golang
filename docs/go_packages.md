# Understanding Go Packages and Modules

Go's approach to code organization is highly opinionated, focusing on simplicity and preventing dependency hell. Here is how packages and modules work in Go:

## 1. Modules (The Global Scope)
A **Module** is a collection of related Go packages that are versioned together as a single unit.
- It is defined by the `go.mod` file at the root of your project.
- Ex: `module github.com/Yakov-Gorochovsky/project`
- This module path dictates how *every single file* inside the project imports other files from the same project.

## 2. Packages (The Local Scope)
A **Package** is a directory containing one or more `.go` files.
- **Rule #1: One Package Per Directory.** Every `.go` file in a specific folder MUST declare the exact same `package <name>` at the top. (e.g., all files in `internal/handler/` must be `package handler`).
- **Rule #2: Import by Path, Use by Name.** When you import a package, you provide the full directory path starting from the module root:
  `import "github.com/Yakov-Gorochovsky/project/internal/handler"`
  But when you use it in the code, you just use the final folder name:
  `handler.NewRouter(...)`

## 3. Visibility (Public vs. Private)
Go does not have `public`, `private`, or `protected` keywords like Java or C#. Instead, visibility is determined by **Capitalization**:
- `var password string` (lowercase first letter) = **Private** to the package. Unusable by other folders.
- `var Password string` (uppercase first letter) = **Public** (Exported). Any other package can import and use it.
- This applies to Functions, Structs, Fields inside Structs, and Variables.

## 4. The Special `internal/` Directory
If a directory is named `internal`, the Go compiler enforces a strict rule: only the code *above* or *next to* the `internal` directory can import its packages. It prevents other developers from importing your private business logic if your code is open-source.

## 5. Dependency Management
- `go get <library>`: Downloads a remote module and adds it to `go.mod`.
- `go mod tidy`: Cleans up the `go.mod` and `go.sum` files. It removes unused dependencies and adds missing ones based on your import statements. Everything is checksummed for security.
