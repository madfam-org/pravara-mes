"use client";

import { useId, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { inventoryAPI, type InventoryItem } from "@/lib/api";
import {
  STOCK_TRANSACTION_TYPES,
  signedStockDelta,
  transactionTypeMeta,
  type StockTransactionType,
} from "@/lib/inventory";
import { cn } from "@/lib/utils";

interface StockAdjustFormProps {
  item: InventoryItem;
  token: string;
  defaultType?: StockTransactionType;
  /** "row" fits a table cell; "card" stacks with visible labels. */
  layout?: "row" | "card";
  onDone: () => void;
}

function formatQty(value: number): string {
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: 4 }).format(
    value
  );
}

/**
 * Records a stock movement for one item. The amount is typed in the user's
 * terms (received, consumed) and sent as the signed delta the API applies,
 * with a preview of on-hand after the movement.
 */
export function StockAdjustForm({
  item,
  token,
  defaultType = "receipt",
  layout = "row",
  onDone,
}: StockAdjustFormProps) {
  const queryClient = useQueryClient();
  const id = useId();
  const [type, setType] = useState<StockTransactionType>(defaultType);
  const [quantity, setQuantity] = useState("");
  const [notes, setNotes] = useState("");

  const meta = transactionTypeMeta(type);
  const delta = signedStockDelta(type, quantity);
  const isCard = layout === "card";

  const mutation = useMutation({
    mutationFn: (signed: number) =>
      inventoryAPI.adjust(token, item.id, {
        quantity: signed,
        transaction_type: type,
        notes: notes.trim() || undefined,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["inventory"] });
      queryClient.invalidateQueries({ queryKey: ["inventory-low-stock"] });
      onDone();
    },
  });

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (delta === null || mutation.isPending) return;
    mutation.mutate(delta);
  };

  const preview =
    delta === null
      ? meta.hint
      : `On hand ${formatQty(item.quantity_on_hand)} → ${formatQty(
          item.quantity_on_hand + delta
        )} ${item.unit}`;

  return (
    <form
      onSubmit={handleSubmit}
      aria-label={`Record stock movement for ${item.name}`}
      className={cn("space-y-1.5", isCard && "space-y-3")}
    >
      <div
        className={cn(
          "flex items-center gap-2",
          isCard && "flex-col items-stretch"
        )}
      >
        <div className={cn(isCard && "space-y-1")}>
          {isCard && (
            <label htmlFor={`${id}-type`} className="text-xs font-medium">
              Movement
            </label>
          )}
          <Select
            value={type}
            onValueChange={(v) => setType(v as StockTransactionType)}
          >
            <SelectTrigger
              id={`${id}-type`}
              aria-label={isCard ? undefined : `Movement type for ${item.name}`}
              className={cn("h-8 text-xs", !isCard && "w-[120px]")}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STOCK_TRANSACTION_TYPES.map((t) => (
                <SelectItem key={t.value} value={t.value}>
                  {t.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className={cn(isCard && "space-y-1")}>
          {isCard && (
            <label htmlFor={`${id}-qty`} className="text-xs font-medium">
              {meta.quantityLabel} ({item.unit})
            </label>
          )}
          <Input
            id={`${id}-qty`}
            type="number"
            inputMode="decimal"
            step="any"
            min={type === "adjustment" ? undefined : 0}
            placeholder={isCard ? undefined : meta.quantityLabel}
            aria-label={
              isCard ? undefined : `${meta.quantityLabel} (${item.unit})`
            }
            aria-describedby={`${id}-preview`}
            value={quantity}
            onChange={(e) => setQuantity(e.target.value)}
            className={cn("h-8 text-xs", !isCard && "w-32")}
            autoFocus
          />
        </div>
        <div className={cn(isCard && "space-y-1")}>
          {isCard && (
            <label htmlFor={`${id}-notes`} className="text-xs font-medium">
              Notes (optional)
            </label>
          )}
          <Input
            id={`${id}-notes`}
            placeholder={isCard ? undefined : "Notes"}
            aria-label={isCard ? undefined : "Notes (optional)"}
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            className={cn("h-8 text-xs", !isCard && "w-28")}
          />
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="submit"
            size="sm"
            className={cn("h-8 text-xs", isCard && "flex-1")}
            disabled={delta === null || mutation.isPending}
          >
            {mutation.isPending ? "Saving..." : "Save"}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size={isCard ? "sm" : "icon"}
            className={cn("h-8", !isCard && "w-8")}
            onClick={onDone}
            aria-label={isCard ? undefined : "Cancel stock movement"}
          >
            {isCard ? "Cancel" : <X className="h-4 w-4" aria-hidden="true" />}
          </Button>
        </div>
      </div>
      <p
        id={`${id}-preview`}
        className="text-xs text-muted-foreground"
        aria-live="polite"
      >
        {preview}
      </p>
      {mutation.isError && (
        <p role="alert" className="text-xs text-destructive">
          Couldn&apos;t record the movement
          {mutation.error instanceof Error && mutation.error.message
            ? `: ${mutation.error.message}`
            : "."}{" "}
          Nothing was changed; try again.
        </p>
      )}
    </form>
  );
}
