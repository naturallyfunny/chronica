package inmemory_test

import (
	"testing"

	"go.naturallyfunny.dev/chronica/inmemory"
	"go.naturallyfunny.dev/chronica/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, inmemory.New)
}
