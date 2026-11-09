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
import { QueryErrorState } from "@/components/query-error-state";
import { BOMSection } from "@/components/products/bom-section";
import { useToast } from "@/components/ui/use-toast";
import { productsAPI, type ProductDefinition } from "@/lib/api";
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
  const { toast } = useToast();

  // Fetch products
  const productsQuery = useQuery({
    queryKey: ["products"],
    queryFn: () => productsAPI.list(token),
    enabled: !!token,
  });
  const { data, isLoading } = productsQuery;

  const products = useMemo(() => data?.data ?? [], [data]);

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
    createMutation.reset();
    updateMutation.reset();
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
    createMutation.reset();
    updateMutation.reset();
    setDialogOpen(true);
  };

  const handleDeleteClick = (product: ProductDefinition) => {
    setProductToDelete(product);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!token || !productToDelete) return;
    try {
      await deleteMutation.mutateAsync(productToDelete.id);
    } catch (error) {
      toast({
        title: "Couldn't delete product",
        description: `${
          error instanceof Error && error.message ? `${error.message}. ` : ""
        }${productToDelete.name} was not deleted; try again.`,
        variant: "destructive",
      });
    }
    setProductToDelete(undefined);
  };

  const handleSubmit = async () => {
    if (!token) return;
    const payload: Partial<ProductDefinition> = {
      ...formData,
      description: formData.description || undefined,
      cad_file_url: formData.cad_file_url || undefined,
    };

    try {
      if (selectedProduct) {
        await updateMutation.mutateAsync({
          id: selectedProduct.id,
          data: payload,
        });
      } else {
        await createMutation.mutateAsync(payload);
      }
    } catch {
      // Shown in the dialog through the mutation's error state.
    }
  };

  const handleToggleExpand = (productId: string) => {
    setExpandedProductId((prev) => (prev === productId ? null : productId));
  };

  const isDialogLoading = createMutation.isPending || updateMutation.isPending;
  const dialogError = selectedProduct
    ? updateMutation.error
    : createMutation.error;

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
      ) : productsQuery.isError ? (
        <QueryErrorState
          what="products"
          error={productsQuery.error}
          onRetry={() => productsQuery.refetch()}
          isRetrying={productsQuery.isFetching}
        />
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
                      <BOMSection product={product} token={token} />
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

          {dialogError && (
            <p role="alert" className="text-sm text-destructive">
              Couldn&apos;t save the product
              {dialogError instanceof Error && dialogError.message
                ? `: ${dialogError.message}`
                : "."}{" "}
              Your changes are kept; try again.
            </p>
          )}

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
