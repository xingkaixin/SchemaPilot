CREATE TABLE IF NOT EXISTS shop.customers (
  id          bigint PRIMARY KEY,
  legacy_code text UNIQUE,
  email       text NOT NULL
);

CREATE TABLE IF NOT EXISTS shop.orders (
  id          bigint PRIMARY KEY,
  customer_id bigint REFERENCES shop.customers (id),
  total_cents bigint,
  created_at  timestamptz NOT NULL DEFAULT now()
);
