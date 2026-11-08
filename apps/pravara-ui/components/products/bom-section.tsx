"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ConfirmDialog } from "@/components/dialogs/confirm-dialog";
import { QueryErrorState } from "@/components/query-error-state";
import { productsAPI, type BOMItem, type ProductDefinition } from "@/lib/api";
import { formatRecordedCost } from "@/lib/inventory";

/** The add form records estimated costs in this currency (it has no picker). */
const BOM_FORM_CURRENCY = "MXN";

interface BOMFormData {
  material_name: string;
  material_code: string;
  quantity: number;
  unit: string;
  estimated_cost: number;
  supplier: string;
}

const defaultBOMFormData: BOMFormData = {
  material_name: "",
  material_code: "",
  quantity: 1,
  unit: "pcs",
  estimated_cost: 0,
  supplier: "",
};

function errorText(error: unknown): string {
  return error instanceof Error && error.message ? `: ${error.message}` : ".";
}

function EstimatedCost({ item }: { item: BOMItem }) {
  if (item.estimated_cost == null) {
    return <span aria-label="No estimated cost">—</span>;
  }
  const { text, currencyKnown } = formatRecordedCost(
    item.estimated_cost,
    item.currency
  );
  return (
    <span>
      {text}
      {!currencyKnown && (
        <span className="block whitespace-nowrap text-xs text-muted-foreground">
          currency not set
        </span>
      )}
    </span>
  );
}

/** Bill of materials for one product: list, add, remove (with confirmation). */
export function BOMSection({
  product,
  token,
}: {
  product: ProductDefinition;
  token: string;
}) {
  const queryClient = useQueryClient();
  const queryKey = ["products", product.id, "bom"];
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<BOMFormData>(defaultBOMFormData);
  const [itemToDelete, setItemToDelete] = useState<BOMItem | undefined>();

  const bomQuery = useQuery({
    queryKey,
    queryFn: () => productsAPI.getBOM(token, product.id),
    enabled: !!token,
  });
  const bomItems = bomQuery.data ?? [];

  const addMutation = useMutation({
    mutationFn: (data: Partial<BOMItem>) =>
      productsAPI.addBOMItem(token, product.id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey });
      setForm(defaultBOMFormData);
      setShowForm(false);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (itemId: string) =>
      productsAPI.deleteBOMItem(token, product.id, itemId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey });
    },
  });

  const closeForm = () => {
    setShowForm(false);
    setForm(defaultBOMFormData);
    addMutation.reset();
  };

  const handleAdd = () => {
    if (!form.material_name || addMutation.isPending) return;
    addMutation.mutate({
      material_name: form.material_name,
      material_code: form.material_code || undefined,
      quantity: form.quantity,
      unit: form.unit,
      estimated_cost: form.estimated_cost || undefined,
      currency: BOM_FORM_CURRENCY,
      supplier: form.supplier || undefined,
    });
  };

  const handleDeleteConfirm = async () => {
    if (!itemToDelete) return;
    try {
      await deleteMutation.mutateAsync(itemToDelete.id);
    } catch {
      // Shown above the table through deleteMutation.isError.
    } finally {
      setItemToDelete(undefined);
    }
  };

  return (
    <div className="mt-6 border-t pt-4">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="text-base font-semibold">Bill of Materials</h3>
        <Button
          size="sm"
          variant="outline"
          onClick={() => (showForm ? closeForm() : setShowForm(true))}
          disabled={bomQuery.isError}
        >
          {showForm ? (
            <>
              <X className="mr-2 h-4 w-4" aria-hidden="true" />
              Cancel
            </>
          ) : (
            <>
              <Plus className="mr-2 h-4 w-4" aria-hidden="true" />
              Add BOM Item
            </>
          )}
        </Button>
      </div>

      {deleteMutation.isError && (
        <p role="alert" className="mb-3 text-sm text-destructive">
          Couldn&apos;t remove the BOM item{errorText(deleteMutation.error)}{" "}
          It is still listed; try again.
        </p>
      )}

      {bomQuery.isLoading ? (
        <div className="space-y-2" aria-busy="true" aria-label="Loading BOM">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-8 w-full animate-pulse rounded bg-muted" />
          ))}
        </div>
      ) : bomQuery.isError ? (
        <QueryErrorState
          inline
          what="the bill of materials"
          error={bomQuery.error}
          onRetry={() => bomQuery.refetch()}
          isRetrying={bomQuery.isFetching}
        />
      ) : bomItems.length > 0 ? (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left">
                <th scope="col" className="pb-2 font-medium text-muted-foreground">
                  Material
                </th>
                <th scope="col" className="pb-2 font-medium text-muted-foreground">
                  Code
                </th>
                <th scope="col" className="pb-2 text-right font-medium text-muted-foreground">
                  Qty
                </th>
                <th scope="col" className="pb-2 pl-3 font-medium text-muted-foreground">
                  Unit
                </th>
                <th scope="col" className="pb-2 text-right font-medium text-muted-foreground">
                  Est. Cost
                </th>
                <th scope="col" className="pb-2 pl-3 font-medium text-muted-foreground">
                  Supplier
                </th>
                <th scope="col" className="w-10 pb-2">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {bomItems.map((item) => (
                <tr key={item.id} className="border-b last:border-0">
                  <td className="py-2 font-medium">{item.material_name}</td>
                  <td className="py-2 font-mono text-xs text-muted-foreground">
                    {item.material_code || "—"}
                  </td>
                  <td className="py-2 text-right tabular-nums">{item.quantity}</td>
                  <td className="py-2 pl-3">{item.unit}</td>
                  <td className="py-2 text-right tabular-nums">
                    <EstimatedCost item={item} />
                  </td>
                  <td className="py-2 pl-3 text-muted-foreground">
                    {item.supplier || "—"}
                  </td>
                  <td className="py-2">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7"
                      onClick={() => {
                        deleteMutation.reset();
                        setItemToDelete(item);
                      }}
                      aria-label={`Remove ${item.material_name}`}
                    >
                      <Trash2
                        className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive"
                        aria-hidden="true"
                      />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        !showForm && (
          <p className="py-4 text-center text-sm text-muted-foreground">
            No BOM items yet. Add materials to define the bill of materials.
          </p>
        )
      )}

      {showForm && (
        <form
          className="mt-4 space-y-4 rounded-lg border bg-muted/30 p-4"
          onSubmit={(e) => {
            e.preventDefault();
            handleAdd();
          }}
        >
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
            <div className="col-span-2 sm:col-span-1">
              <Label htmlFor={`bom-material-name-${product.id}`}>Material Name</Label>
              <Input
                id={`bom-material-name-${product.id}`}
                placeholder="PLA Filament"
                value={form.material_name}
                onChange={(e) => setForm({ ...form, material_name: e.target.value })}
                required
              />
            </div>
            <div>
              <Label htmlFor={`bom-material-code-${product.id}`}>Code</Label>
              <Input
                id={`bom-material-code-${product.id}`}
                placeholder="MAT-001"
                value={form.material_code}
                onChange={(e) => setForm({ ...form, material_code: e.target.value })}
              />
            </div>
            <div>
              <Label htmlFor={`bom-quantity-${product.id}`}>Quantity</Label>
              <Input
                id={`bom-quantity-${product.id}`}
                type="number"
                min={0}
                step="0.01"
                value={form.quantity}
                onChange={(e) =>
                  setForm({ ...form, quantity: e.target.valueAsNumber || 0 })
                }
              />
            </div>
            <div>
              <Label htmlFor={`bom-unit-${product.id}`}>Unit</Label>
              <Input
                id={`bom-unit-${product.id}`}
                placeholder="pcs"
                value={form.unit}
                onChange={(e) => setForm({ ...form, unit: e.target.value })}
              />
            </div>
            <div>
              <Label htmlFor={`bom-cost-${product.id}`}>
                Est. Cost ({BOM_FORM_CURRENCY})
              </Label>
              <Input
                id={`bom-cost-${product.id}`}
                type="number"
                min={0}
                step="0.01"
                placeholder="Optional"
                value={form.estimated_cost || ""}
                onChange={(e) =>
                  setForm({ ...form, estimated_cost: e.target.valueAsNumber || 0 })
                }
              />
            </div>
            <div>
              <Label htmlFor={`bom-supplier-${product.id}`}>Supplier</Label>
              <Input
                id={`bom-supplier-${product.id}`}
                placeholder="Supplier name"
                value={form.supplier}
                onChange={(e) => setForm({ ...form, supplier: e.target.value })}
              />
            </div>
          </div>
          {addMutation.isError && (
            <p role="alert" className="text-sm text-destructive">
              Couldn&apos;t add the BOM item{errorText(addMutation.error)} Your
              entries are kept; try again.
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" size="sm" onClick={closeForm}>
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={!form.material_name || addMutation.isPending}
            >
              {addMutation.isPending ? "Adding..." : "Add Item"}
            </Button>
          </div>
        </form>
      )}

      <ConfirmDialog
        open={!!itemToDelete}
        onOpenChange={(open) => {
          if (!open) setItemToDelete(undefined);
        }}
        title="Remove BOM item"
        description={`Remove ${itemToDelete?.material_name ?? "this item"} from the bill of materials of ${product.name}? This cannot be undone.`}
        onConfirm={handleDeleteConfirm}
        variant="destructive"
      />
    </div>
  );
}
