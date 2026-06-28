ALTER TABLE users ADD COLUMN password_hash text;
ALTER TABLE users DROP CONSTRAINT users_provider_check;
ALTER TABLE users ADD CONSTRAINT users_provider_check
  CHECK (provider IN ('google','apple','password','dummy'));
