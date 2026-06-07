package inmemory_test

import (
	"testing"

	"go.naturallyfunny.dev/chronica/inmemory"
	"go.naturallyfunny.dev/chronica/storeconformance"
)

func TestConformance(t *testing.T) {
	storeconformance.RunTest(t, inmemory.NewStore)
	storeconformance.RunIdempotentTest(t, inmemory.NewIdempotentStore)
}
