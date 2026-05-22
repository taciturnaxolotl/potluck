package web

import "database/sql"

func sqlNullInt64(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

func sqlNullInt64Ptr(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}
