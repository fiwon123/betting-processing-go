CREATE UNIQUE INDEX wager_transactions_one_successful_reversal_uq
ON wager_transactions (internal_reference)
WHERE internal_reference IS NOT NULL
  AND transaction_type IN ('REFUND', 'ROLLBACK')
  AND status = 'PROCESSED';
