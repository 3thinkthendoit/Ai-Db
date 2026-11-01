// Package app is the transport-agnostic application layer shared by the desktop
// (Wails bridge) and server (HTTP) adapters. Every method takes the caller
// Principal so state can be partitioned per user.
package app

// Principal identifies the caller of a service method. The desktop adapter
// always uses LocalPrincipal(); the server adapter derives it from the
// authenticated session.
type Principal struct {
	UserID   string
	Username string
	Role     string // "admin" | "user"
}

// LocalPrincipal is the implicit single-user identity for the desktop build,
// where there is no login and all state belongs to one local owner.
func LocalPrincipal() Principal {
	return Principal{UserID: "local", Username: "local", Role: "admin"}
}
