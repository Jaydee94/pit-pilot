ALTER TABLE users DROP CONSTRAINT users_provider_check;
ALTER TABLE users ADD CONSTRAINT users_provider_check
  CHECK (provider IN ('google','apple'));
ALTER TABLE users DROP COLUMN password_hash;
