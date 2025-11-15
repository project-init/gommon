package postgres

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func UuidStringAsPGUuid(userUuid string) (pgtype.UUID, error) {
	userUuidAsUuid, err := uuid.Parse(userUuid)
	if err != nil {
		return pgtype.UUID{}, err
	}

	return pgtype.UUID{Bytes: userUuidAsUuid, Valid: true}, nil
}
