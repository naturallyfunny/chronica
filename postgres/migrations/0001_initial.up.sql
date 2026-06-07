CREATE TABLE IF NOT EXISTS "chronica" (
    id               TEXT        NOT NULL PRIMARY KEY,
    owner_id         TEXT        NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    last_activity_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS "acta" (
    seq             BIGSERIAL   NOT NULL PRIMARY KEY,
    id              TEXT        NOT NULL,
    chronicum_id    TEXT        NOT NULL REFERENCES "chronica"(id) ON DELETE CASCADE,
    kind            TEXT        NOT NULL,
    actor_kind      TEXT        NOT NULL,
    actor           TEXT        NOT NULL,
    content         TEXT        NOT NULL DEFAULT '',
    at              TIMESTAMPTZ NOT NULL,
    idempotency_key TEXT
);

CREATE INDEX IF NOT EXISTS "idx_acta_chronicum_seq"
    ON "acta" (chronicum_id, seq);

CREATE UNIQUE INDEX IF NOT EXISTS "uq_acta_idempotency"
    ON "acta" (chronicum_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
