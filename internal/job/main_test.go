package job

import (
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestMain(m *testing.M) {
	managedHelperMain()
	fakecli.RunTests(m)
}
