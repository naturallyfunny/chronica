package inmemory_test

import (
	"testing"

	"go.naturallyfunny.dev/chronica/chronicatest"
	"go.naturallyfunny.dev/chronica/inmemory"
)

func TestConformance(t *testing.T) {
	chronicatest.RunConformance(t, inmemory.New)
}
