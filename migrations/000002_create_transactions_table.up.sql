CREATE TABLE transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    amount INTEGER NOT NULL,
    type VARCHAR(20) NOT NULL CHECK (type IN ('deposit', 'withdrawal')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ, -- NULL для withdrawal, дата для deposit
    remaining INTEGER NOT NULL DEFAULT 0, -- остаток для начислений
    description TEXT
);

CREATE INDEX idx_transactions_user_id ON transactions(user_id);
CREATE INDEX idx_transactions_expires_at ON transactions(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX idx_transactions_remaining ON transactions(remaining) WHERE remaining > 0;