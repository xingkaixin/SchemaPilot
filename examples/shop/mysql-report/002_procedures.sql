DROP PROCEDURE IF EXISTS record_sale;

DELIMITER $$
CREATE PROCEDURE record_sale(IN sale_day DATE, IN cents BIGINT)
BEGIN
  INSERT INTO daily_sales (day, total_cents) VALUES (sale_day, cents)
  ON DUPLICATE KEY UPDATE total_cents = total_cents + cents;
END$$
DELIMITER ;

CALL record_sale(CURRENT_DATE, 1200);
