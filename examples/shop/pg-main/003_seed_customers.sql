INSERT INTO shop.customers (id, legacy_code, email)
SELECT n, 'C' || n, 'user' || n || '@example.com'
FROM generate_series(1, 1000) AS n
ON CONFLICT (id) DO NOTHING;
