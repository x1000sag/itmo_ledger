CREATE TABLE IF NOT EXISTS transactions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL,
    amount int NOT NULL CHECK (amount > 0),
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    expires_at timestamp(0) with time zone NOT NULL,
    remaining_amount int NOT NULL CHECK (remaining_amount >= 0),
    CONSTRAINT remaining_amount_check CHECK (remaining_amount <= amount)
);

CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_transactions_user_expires ON transactions(user_id, expires_at) WHERE remaining_amount > 0;
