CREATE TABLE IF NOT EXISTS daily_sales (
  day         DATE PRIMARY KEY,
  total_cents BIGINT NOT NULL DEFAULT 0
);
