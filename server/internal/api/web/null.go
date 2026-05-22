package web

import "database/sql"

func sqlNullInt64(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

func sqlNullInt64Ptr(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

// toInt64 unboxes the interface{} sqlc emits for SUM/COALESCE aggregates.
func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case nil:
		return 0
	}
	return 0
}
