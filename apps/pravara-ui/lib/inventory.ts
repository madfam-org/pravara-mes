/**
 * Stock-movement helpers for the inventory UI.
 *
 * The API applies an adjustment's quantity as a signed delta to
 * quantity_on_hand, and job consumption posts consumption as a negative
 * quantity. The form asks for an amount in the user's terms (received,
 * consumed) and these helpers turn it into the signed delta the API expects.
 */

export type StockTransactionType = "receipt" | "consumption" | "adjustment";

export const STOCK_TRANSACTION_TYPES: ReadonlyArray<{
  value: StockTransactionType;
  label: string;
  quantityLabel: string;
  hint: string;
}> = [
  {
    value: "receipt",
    label: "Receipt",
    quantityLabel: "Quantity received",
    hint: "Adds to on hand",
  },
  {
    value: "consumption",
    label: "Consumption",
    quantityLabel: "Quantity consumed",
    hint: "Removes from on hand",
  },
  {
    value: "adjustment",
    label: "Adjustment",
    quantityLabel: "Change (+/−)",
    hint: "Positive adds, negative removes",
  },
];

export function transactionTypeMeta(type: StockTransactionType) {
  return (
    STOCK_TRANSACTION_TYPES.find((t) => t.value === type) ??
    STOCK_TRANSACTION_TYPES[0]
  );
}

/**
 * The signed delta to send for an amount typed into the form, or null when
 * the amount cannot be recorded (empty, not a number, zero).
 */
export function signedStockDelta(
  type: StockTransactionType,
  raw: string
): number | null {
  if (raw.trim() === "") return null;
  const value = Number(raw);
  if (!Number.isFinite(value) || value === 0) return null;
  switch (type) {
    case "receipt":
      return Math.abs(value);
    case "consumption":
      return -Math.abs(value);
    default:
      return value;
  }
}

/**
 * A reference cost formatted with the currency recorded next to it. When no
 * currency was recorded the amount is shown as a plain number and
 * currencyKnown is false, so the UI can say so instead of assuming one.
 */
export function formatRecordedCost(
  amount: number,
  currency?: string | null
): { text: string; currencyKnown: boolean } {
  const code = currency?.trim().toUpperCase();
  if (code && /^[A-Z]{3}$/.test(code)) {
    try {
      return {
        text: new Intl.NumberFormat("en-US", {
          style: "currency",
          currency: code,
        }).format(amount),
        currencyKnown: true,
      };
    } catch {
      // Unknown ISO code: fall through to the plain number.
    }
  }
  return {
    text: new Intl.NumberFormat("en-US", {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount),
    currencyKnown: false,
  };
}
