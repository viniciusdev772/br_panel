package deploy

import (
	"os"
	"testing"
)

// The tests pick ports like 3001, which may well be serving a real app on the machine that runs
// them (a production server, a developer's laptop). By default nothing counts as busy; a test that
// cares sets portBusy itself.
func TestMain(m *testing.M) {
	portBusy = func(int) bool { return false }
	os.Exit(m.Run())
}
