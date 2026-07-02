ALTER TABLE symbol_configs ADD COLUMN IF NOT EXISTS price_digits integer NOT NULL DEFAULT 5;
