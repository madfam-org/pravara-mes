"use client";

import { useState, useMemo } from "react";
import { usePravaraSession } from "@/lib/auth";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Plus,
  Search,
  Package,
  ChevronDown,
  ChevronRight,
  Trash2,
  Edit,
  MoreVertical,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SearchInput } from "@/components/search-input";
import { ConfirmDialog } from "@/components/dialogs/confirm-dialog";
import {
  productsAPI,
  type ProductDefinition,
  type BOMItem,
  type ListResponse,
} from "@/lib/api";
import { formatDate } from "@/lib/utils";

type ProductCategory = ProductDefinition["category"];

const categoryColors: Record<ProductCategory, string> = {
  "3d_print":
    "bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400",
  cnc_part:
    "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
  laser_cut:
    "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400",
  assembly:
    "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",
  other: "bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400",
};

const categoryLabels: Record<ProductCategory, string> = {
  "3d_print": "3D Print",
  cnc_part: "CNC Part",
  laser_cut: "Laser Cut",
  assembly: "Assembly",
  other: "Other",
};

const CATEGORIES: ProductCategory[] = [
  "3d_print",
  "cnc_part",
  "laser_cut",
  "assembly",
  "other",
];

interface ProductFormData {
  sku: string;
  name: string;
  version: string;
  category: ProductCategory;
  description: string;
  cad_file_url: string;
  is_active: boolean;
}

const defaultFormData: ProductFormData = {
  sku: "",
  name: "",
  version: "1.0",
  category: "other",
  description: "",
  cad_file_url: "",
  is_active: true,
};

interface BOMFormData {
  material_name: string;
  material_code: string;
  quantity: number;
  unit: string;
  estimated_cost: number;
  currency: string;
  supplier: string;
}

const defaultBOMFormData: BOMFormData = {
  material_name: "",
  material_code: "",
  quantity: 1,
  unit: "pcs",
  estimated_cost: 0,
  currency: "MXN",
  supplier: "",
};

export default function ProductsPage() {
  const { data: session } = usePravaraSession();
  const token = (session?.user as any)?.accessToken;
  const queryClient = useQueryClient();

  const [searchQuery, setSearchQuery] = useState("");
  const [expandedProductId, setExpandedProductId] = useState<string | null>(
    null
  );
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedProduct, setSelectedProduct] = useState<
    ProductDefinition | undefined
  >();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [productToDelete, setProductToDelete] = useState<
    ProductDefinition | undefined
  >();
  const [formData, setFormData] = useState<ProductFormData>(defaultFormData);
  const [bomFormData, setBomFormData] =
    useState<BOMFormData>(defaultBOMFormData);
  const [showBomForm, setShowBomForm] = useState(false);

  // Fetch products
  const { data, isLoading } = useQuery({
    queryKey: ["products"],
    queryFn: () => productsAPI.list(token),
    enabled: !!token,
  });

  const products = data?.data || [];

  // Fetch BOM for expanded product
  const { data: bomItems } = useQuery({
    queryKey: ["products", expandedProductId, "bom"],
    queryFn: () => productsAPI.getBOM(token, expandedProductId!),
    enabled: !!token && !!expandedProductId,
  });

  // Mutations
  const createMutation = useMutation({
    mutationFn: (data: Partial<ProductDefinition>) =>
      productsAPI.create(token, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["products"] });
      setDialogOpen(false);
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string;
      data: Partial<ProductDefinition>;
    }) => productsAPI.update(token, id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["products"] });
      setDialogOpen(false);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => productsAPI.delete(token, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["products"] });
      setProductToDelete(undefined);
    },
  });

  const addBomItemMutation = useMutation({
    mutationFn: ({
      productId,
      data,
    }: {
      productId: string;
      data: Partial<BOMItem>;
    }) => productsAPI.addBOMItem(token, productId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["products", expandedProductId, "bom"],
      });
      setBomFormData(defaultBOMFormData);
      setShowBomForm(false);
    },
  });

  const deleteBomItemMutation = useMutation({
    mutationFn: ({
      productId,
      itemId,
    }: {
      productId: string;
      itemId: string;
    }) => productsAPI.deleteBOMItem(token, productId, itemId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["products", expandedProductId, "bom"],
      });
    },
  });

  // Filter products
  const filteredProducts = useMemo(() => {
    if (!searchQuery) return products;
    const query = searchQuery.toLowerCase();
    return products.filter(
      (p) =>
        p.name.toLowerCase().includes(query) ||
        p.sku.toLowerCase().includes(query)
    );
  }, [products, searchQuery]);

  const handleNewProduct = () => {
    setSelectedProduct(undefined);
    setFormData(defaultFormData);
    setDialogOpen(true);
  };

  const handleEditProduct = (product: ProductDefinition) => {
    setSelectedProduct(product);
    setFormData({
      sku: product.sku,
      name: product.name,
      version: product.version,
      category: product.category,
      description: product.description || "",
      cad_file_url: product.cad_file_url || "",
      is_active: product.is_active,
    });
    setDialogOpen(true);
  };

  const handleDeleteClick = (product: ProductDefinition) => {
    setProductToDelete(product);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!token || !productToDelete) return;
    await deleteMutation.mutateAsync(productToDelete.id);
    setProductToDelete(undefined);
  };

  const handleSubmit = async () => {
    if (!token) return;
    const payload: Partial<ProductDefinition> = {
      ...formData,
      description: formData.description || undefined,
      cad_file_url: formData.cad_file_url || undefined,
    };

    if (selectedProduct) {
      await updateMutation.mutateAsync({
        id: selectedProduct.id,
        data: payload,
      });
    } else {
      await createMutation.mutateAsync(payload);
    }
  };

  const handleToggleExpand = (productId: string) => {
    setExpandedProductId((prev) => (prev === productId ? null : productId));
    setShowBomForm(false);
    setBomFormData(defaultBOMFormData);
  };

  const handleAddBomItem = async () => {
    if (!expandedProductId || !token) return;
    await addBomItemMutation.mutateAsync({
      productId: expandedProductId,
      data: {
        material_name: bomFormData.material_name,
        material_code: bomFormData.material_code || undefined,
        quantity: bomFormData.quantity,
        unit: bomFormData.unit,
        estimated_cost: bomFormData.estimated_cost || undefined,
        currency: bomFormData.currency,
        supplier: bomFormData.supplier || undefined,
      },
    });
  };

  const isDialogLoading = createMutation.isPending || updateMutation.isPending;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Products</h1>
          <p className="text-muted-foreground">
            Product catalog and bill of materials
          </p>
        </div>
        <Button size="sm" onClick={handleNewProduct}>
          <Plus className="mr-2 h-4 w-4" />
          New Product
        </Button>
      </div>

      {/* Search */}
      <SearchInput
        placeholder="Search by SKU or name..."
        value={searchQuery}
        onChange={setSearchQuery}
        className="w-full sm:w-80"
      />

      {/* Loading state */}
      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(6)].map((_, i) => (
            <Card key={i} className="animate-pulse">
              <CardHeader>
                <div className="h-5 w-32 rounded bg-muted" />
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="h-4 w-full rounded bg-muted" />
                  <div className="h-4 w-2/3 rounded bg-muted" />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      ) : filteredProducts.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <Package className="h-12 w-12 text-muted-foreground" />
            <h3 className="mt-4 text-lg font-semibold">
              {products.length === 0
                ? "No products yet"
                : "No matching products"}
            </h3>
            <p className="text-muted-foreground">
              {products.length === 0
                ? "Create your first product to get started"
                : "Try adjusting your search"}
            </p>
            {products.length === 0 && (
              <Button className="mt-4" onClick={handleNewProduct}>
                <Plus className="mr-2 h-4 w-4" />
                Create Product
              </Button>
            )}
            {products.length > 0 && searchQuery && (
              <Button
                variant="outline"
                className="mt-4"
                onClick={() => setSearchQuery("")}
              >
                Clear search
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <>
          {searchQuery && (
            <p className="text-sm text-muted-foreground">
              Showing {filteredProducts.length} of {products.length} products
            </p>
          )}

          {/* Product grid */}
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            {filteredProducts.map((product) => {
              const isExpanded = expandedProductId === product.id;

              return (
                <Card
                  key={product.id}
                  className={`hover:shadow-md transition-shadow ${
                    isExpanded ? "md:col-span-2 lg:col-span-3" : ""
                  }`}
                >
                  <CardHeader className="pb-2">
                    <div className="flex items-start justify-between">
                      <div
                        className="flex-1 cursor-pointer"
                        onClick={() => handleToggleExpand(product.id)}
                        role="button"
                        tabIndex={0}
                        aria-expanded={isExpanded}
                        aria-label={`Toggle BOM for ${product.name}`}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            handleToggleExpand(product.id);
                          }
                        }}
                      >
                        <div className="flex items-center gap-2">
                          {isExpanded ? (
                            <ChevronDown className="h-4 w-4 text-muted-foreground" />
                          ) : (
                            <ChevronRight className="h-4 w-4 text-muted-foreground" />
                          )}
                          <CardTitle className="text-lg">
                            {product.name}
                          </CardTitle>
                        </div>
                        <p className="text-sm font-mono text-muted-foreground ml-6">
                          {product.sku}
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        <span
                          className={`inline-flex items-center rounded-full px-2 py-1 text-xs font-medium ${
                            categoryColors[product.category]
                          }`}
                        >
                          {categoryLabels[product.category]}
                        </span>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8"
                              aria-label={`Actions for ${product.name}`}
                            >
                              <MoreVertical className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem
                              onClick={() => handleEditProduct(product)}
                            >
                              <Edit className="mr-2 h-4 w-4" />
                              Edit
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              onClick={() => handleDeleteClick(product)}
                              className="text-destructive"
                            >
                              <Trash2 className="mr-2 h-4 w-4" />
                              Delete
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-2 text-sm">
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Version</span>
                        <span className="font-medium">{product.version}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Status</span>
                        <Badge
                          variant={product.is_active ? "success" : "secondary"}
                        >
                          {product.is_active ? "Active" : "Inactive"}
                        </Badge>
                      </div>
                      {product.description && (
                        <p className="text-muted-foreground line-clamp-2 pt-1">
                          {product.description}
                        </p>
                      )}
                      <p className="text-xs text-muted-foreground pt-2">
                        Created {formatDate(product.created_at)}
                      </p>
                    </div>

                    {/* BOM Section (expanded) */}
                    {isExpanded && (
                      <div className="mt-6 border-t pt-4">
                        <div className="flex items-center justify-between mb-4">
                          <h3 className="text-base font-semibold">
                            Bill of Materials
                          </h3>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => setShowBomForm(!showBomForm)}
                          >
                            {showBomForm ? (
                              <>
                                <X className="mr-2 h-4 w-4" />
                                Cancel
                              </>
                            ) : (
                              <>
                                <Plus className="mr-2 h-4 w-4" />
                                Add BOM Item
                              </>
                            )}
                          </Button>
                        </div>

                        {/* BOM Table */}
                        {bomItems && bomItems.length > 0 ? (
                          <div className="overflow-x-auto">
                            <table className="w-full text-sm">
                              <thead>
                                <tr className="border-b text-left">
                                  <th className="pb-2 font-medium text-muted-foreground">
                                    Material
                                  </th>
                                  <th className="pb-2 font-medium text-muted-foreground">
                                    Code
                                  </th>
                                  <th className="pb-2 font-medium text-muted-foreground text-right">
                                    Qty
                                  </th>
                                  <th className="pb-2 font-medium text-muted-foreground">
                                    Unit
                                  </th>
                                  <th className="pb-2 font-medium text-muted-foreground text-right">
                                    Est. Cost
                                  </th>
                                  <th className="pb-2 font-medium text-muted-foreground">
                                    Supplier
                                  </th>
                                  <th className="pb-2 w-10" />
                                </tr>
                              </thead>
                              <tbody>
                                {bomItems.map((item) => (
                                  <tr
                                    key={item.id}
                                    className="border-b last:border-0"
                                  >
                                    <td className="py-2 font-medium">
                                      {item.material_name}
                                    </td>
                                    <td className="py-2 font-mono text-xs text-muted-foreground">
                                      {item.material_code || "-"}
                                    </td>
                                    <td className="py-2 text-right">
                                      {item.quantity}
                                    </td>
                                    <td className="py-2">{item.unit}</td>
                                    <td className="py-2 text-right">
                                      {item.estimated_cost != null
                                        ? new Intl.NumberFormat("en-US", {
                                            style: "currency",
                                            currency: item.currency || "MXN",
                                          }).format(item.estimated_cost)
                                        : "-"}
                                    </td>
                                    <td className="py-2 text-muted-foreground">
                                      {item.supplier || "-"}
                                    </td>
                                    <td className="py-2">
                                      <Button
                                        variant="ghost"
                                        size="icon"
                                        className="h-7 w-7"
                                        onClick={() =>
                                          deleteBomItemMutation.mutate({
                                            productId: product.id,
                                            itemId: item.id,
                                          })
                                        }
                                        aria-label={`Remove ${item.material_name}`}
                                      >
                                        <Trash2 className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
                                      </Button>
                                    </td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                        ) : (
                          !showBomForm && (
                            <p className="text-sm text-muted-foreground text-center py-4">
                              No BOM items yet. Add materials to define the bill
                              of materials.
                            </p>
                          )
                        )}

                        {/* Inline Add BOM Form */}
                        {showBomForm && (
                          <div className="mt-4 rounded-lg border p-4 space-y-4 bg-muted/30">
                            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
                              <div className="col-span-2 sm:col-span-1">
                                <Label htmlFor="bom-material-name">
                                  Material Name
                                </Label>
                                <Input
                                  id="bom-material-name"
                                  placeholder="PLA Filament"
                                  value={bomFormData.material_name}
                                  onChange={(e) =>
                                    setBomFormData({
                                      ...bomFormData,
                                      material_name: e.target.value,
                                    })
                                  }
                                />
                              </div>
                              <div>
                                <Label htmlFor="bom-material-code">Code</Label>
                                <Input
                                  id="bom-material-code"
                                  placeholder="MAT-001"
                                  value={bomFormData.material_code}
                                  onChange={(e) =>
                                    setBomFormData({
                                      ...bomFormData,
                                      material_code: e.target.value,
                                    })
                                  }
                                />
                              </div>
                              <div>
                                <Label htmlFor="bom-quantity">Quantity</Label>
                                <Input
                                  id="bom-quantity"
                                  type="number"
                                  min={0}
                                  step="0.01"
                                  value={bomFormData.quantity}
                                  onChange={(e) =>
                                    setBomFormData({
                                      ...bomFormData,
                                      quantity: e.target.valueAsNumber || 0,
                                    })
                                  }
                                />
                              </div>
                              <div>
                                <Label htmlFor="bom-unit">Unit</Label>
                                <Input
                                  id="bom-unit"
                                  placeholder="pcs"
                                  value={bomFormData.unit}
                                  onChange={(e) =>
                                    setBomFormData({
                                      ...bomFormData,
                                      unit: e.target.value,
                                    })
                                  }
                                />
                              </div>
                              <div>
                                <Label htmlFor="bom-cost">Est. Cost</Label>
                                <Input
                                  id="bom-cost"
                                  type="number"
                                  min={0}
                                  step="0.01"
                                  placeholder="0.00"
                                  value={bomFormData.estimated_cost || ""}
                                  onChange={(e) =>
                                    setBomFormData({
                                      ...bomFormData,
                                      estimated_cost:
                                        e.target.valueAsNumber || 0,
                                    })
                                  }
                                />
                              </div>
                              <div>
                                <Label htmlFor="bom-supplier">Supplier</Label>
                                <Input
                                  id="bom-supplier"
                                  placeholder="Supplier name"
                                  value={bomFormData.supplier}
                                  onChange={(e) =>
                                    setBomFormData({
                                      ...bomFormData,
                                      supplier: e.target.value,
                                    })
                                  }
                                />
                              </div>
                            </div>
                            <div className="flex justify-end gap-2">
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => {
                                  setShowBomForm(false);
                                  setBomFormData(defaultBOMFormData);
                                }}
                              >
                                Cancel
                              </Button>
                              <Button
                                size="sm"
                                onClick={handleAddBomItem}
                                disabled={
                                  !bomFormData.material_name ||
                                  addBomItemMutation.isPending
                                }
                              >
                                {addBomItemMutation.isPending
                                  ? "Adding..."
                                  : "Add Item"}
                              </Button>
                            </div>
                          </div>
                        )}
                      </div>
                    )}
                  </CardContent>
                </Card>
              );
            })}
          </div>
        </>
      )}

      {/* Create / Edit Product Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {selectedProduct ? "Edit Product" : "Create Product"}
            </DialogTitle>
            <DialogDescription>
              {selectedProduct
                ? "Update the product details below."
                : "Fill in the details to create a new product definition."}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="col-span-2">
                <Label htmlFor="product-name">Product Name</Label>
                <Input
                  id="product-name"
                  placeholder="Widget Assembly v2"
                  value={formData.name}
                  onChange={(e) =>
                    setFormData({ ...formData, name: e.target.value })
                  }
                />
              </div>
              <div>
                <Label htmlFor="product-sku">SKU</Label>
                <Input
                  id="product-sku"
                  placeholder="WDG-ASM-002"
                  value={formData.sku}
                  onChange={(e) =>
                    setFormData({ ...formData, sku: e.target.value })
                  }
                />
              </div>
              <div>
                <Label htmlFor="product-version">Version</Label>
                <Input
                  id="product-version"
                  placeholder="1.0"
                  value={formData.version}
                  onChange={(e) =>
                    setFormData({ ...formData, version: e.target.value })
                  }
                />
              </div>
              <div>
                <Label htmlFor="product-category">Category</Label>
                <Select
                  value={formData.category}
                  onValueChange={(value) =>
                    setFormData({
                      ...formData,
                      category: value as ProductCategory,
                    })
                  }
                >
                  <SelectTrigger id="product-category">
                    <SelectValue placeholder="Select category" />
                  </SelectTrigger>
                  <SelectContent>
                    {CATEGORIES.map((cat) => (
                      <SelectItem key={cat} value={cat}>
                        {categoryLabels[cat]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div>
                <Label htmlFor="product-cad-url">CAD File URL (Optional)</Label>
                <Input
                  id="product-cad-url"
                  placeholder="https://..."
                  value={formData.cad_file_url}
                  onChange={(e) =>
                    setFormData({ ...formData, cad_file_url: e.target.value })
                  }
                />
              </div>
              <div className="col-span-2">
                <Label htmlFor="product-description">
                  Description (Optional)
                </Label>
                <Textarea
                  id="product-description"
                  placeholder="Product description..."
                  value={formData.description}
                  onChange={(e) =>
                    setFormData({ ...formData, description: e.target.value })
                  }
                  rows={3}
                />
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setDialogOpen(false)}
              disabled={isDialogLoading}
            >
              Cancel
            </Button>
            <Button
              onClick={handleSubmit}
              disabled={!formData.name || !formData.sku || isDialogLoading}
            >
              {isDialogLoading
                ? "Saving..."
                : selectedProduct
                ? "Update Product"
                : "Create Product"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation */}
      <ConfirmDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        title="Delete Product"
        description={`Are you sure you want to delete ${productToDelete?.name} (${productToDelete?.sku})? This will also remove all BOM items. This action cannot be undone.`}
        onConfirm={handleDeleteConfirm}
        variant="destructive"
      />
    </div>
  );
}
