//go:build windows

package embedded

// embeddedPostgresSupported is false on Windows, and StartPostgres refuses there.
//
// The package now compiles for Windows, but compiling is not supporting: process
// liveness (processAlive, signal 0) and identity (processIsPostgresFor, which
// shells out to `ps`) are both Unix-only. On Windows they would report a live
// postmaster as gone, so shutdown would claim success, release the ownership
// lock, and leave PostgreSQL running -- a silent failure, which is worse than
// the build error this replaced.
//
// Nothing ships embedded Postgres on Windows today: there is no binaries bundle
// and no packaging target. Refusing explicitly states that, instead of leaving a
// path that looks supported and misbehaves. Lift this together with Windows
// implementations of liveness and identity.
const embeddedPostgresSupported = false
