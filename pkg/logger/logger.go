// Package logger provides a simple logging utility.
//
// Unlike the 'internal' directory, code placed in the 'pkg' directory
// is meant to be exported and used by OTHER projects. If you were to
// open-source this project or share it across your organization,
// other teams could import your 'pkg/logger' package safely.
//
// Rule of thumb: IF it's only for this specific application, put it in 'internal/'.
// IF you want others to import it as a library, put it in 'pkg/'.
// NOTE: Many modern Go projects don't use 'pkg' at all and just put
// exportable packages in the root directory. 'pkg' is a community convention.
package logger

import "log"

// Info logs an informational message.
func Info(msg string) {
	log.Println("[INFO]", msg)
}
