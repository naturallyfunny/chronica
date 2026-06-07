package postgres

import "go.naturallyfunny.dev/chronica"

func ExportBuildActaQuery(chronicumID string, q chronica.ActaQuery) (string, []any) {
	return buildActaQuery(chronicumID, q)
}
