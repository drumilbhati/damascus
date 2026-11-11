ALTER TABLE observations ALTER COLUMN load_rate TYPE INT USING ROUND(load_rate)::INT;
