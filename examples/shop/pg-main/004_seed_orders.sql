INSERT INTO shop.orders (id, customer_id, total_cents)
SELECT n, (n % 1000) + 1, n * 100
FROM generate_series(1, 10000) AS n
ON CONFLICT (id) DO NOTHING;
