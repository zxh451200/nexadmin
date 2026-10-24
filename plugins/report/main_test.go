package report

import (
	"os"
	"testing"

	"nexadmin/pkg/idgen"
)

func TestMain(m *testing.M) {
	if err := idgen.Init(1); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
