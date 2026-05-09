import type { ReceiptStatus } from "../src/receipt.ts";
import { RECEIPT_STATUS_VALUES } from "../src/receipt-literals.ts";

const VALID_RECEIPT_STATUSES: readonly ReceiptStatus[] = RECEIPT_STATUS_VALUES;

// @ts-expect-error invalid receipt status literals must fail typecheck.
const INVALID_RECEIPT_STATUSES = ["ok", "deferred"] as const satisfies readonly ReceiptStatus[];

void VALID_RECEIPT_STATUSES;
void INVALID_RECEIPT_STATUSES;
