package eval

import "time"

// nowUTC is a test seam for the current time.
var nowUTC = func() time.Time {
	return time.Now().UTC()
}
