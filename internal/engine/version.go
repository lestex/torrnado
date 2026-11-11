package engine

import "fmt"

// VersionSkew compares the build a client is with the one a daemon said it
// was, in GlobalStats.Version.
//
// It exists because the daemon outlives its clients by design, so a client
// built this afternoon routinely talks to a daemon started last week, and
// the wire format has no handshake to notice. A field the old daemon has
// never heard of simply arrives as its zero value: something quietly
// missing, not an error. The one cure is restarting the daemon, and
// nothing else says that it is needed.
//
// Both sides build their string with the same function, so the same
// binary always produces the same text and anything else is a different
// build. That is stricter than the wire needs - a patch release is often
// compatible - and deliberately so: the documented advice after any
// upgrade is to restart the daemon, and this says when that has not
// happened.
//
// The note reads as the second half of a sentence ("daemon is X, this is
// Y"), and is empty when there is nothing to say.
func VersionSkew(client, daemon string) (note string, skewed bool) {
	switch {
	case client == "":
		// A client that cannot say what it is has nothing to compare, and
		// guessing would warn every test and every embedder.
		return "", false
	case daemon == "":
		// Empty is what a daemon older than the field reports, and a
		// daemon that old is by definition not this build.
		return fmt.Sprintf("daemon is too old to report its version, this is %s", client), true
	case daemon == client:
		return "", false
	}
	return fmt.Sprintf("daemon is %s, this is %s", daemon, client), true
}
