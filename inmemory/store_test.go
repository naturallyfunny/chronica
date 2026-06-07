package inmemory_test

import (
	"testing"

	"go.naturallyfunny.dev/chronica/inmemory"
	"go.naturallyfunny.dev/chronica/storeconformance"
)

func TestConformance(t *testing.T) {
	storeconformance.Run(t, inmemory.NewStore)
}
