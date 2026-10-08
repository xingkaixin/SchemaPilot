CREATE OR REPLACE FUNCTION shop.customer_total(customer bigint) RETURNS bigint AS $$
BEGIN
  RETURN (SELECT coalesce(sum(total_cents), 0) FROM shop.orders WHERE customer_id = customer);
END;
$$ LANGUAGE plpgsql;

ANALYZE shop.orders;
