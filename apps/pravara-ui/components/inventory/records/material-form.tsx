"use client";

import { useId, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  MATERIAL_CATEGORIES,
  materialsAPI,
  type Material,
  type MaterialCategory,
  type MaterialPatch,
} from "@/lib/inventory-records-api";
import {
  CATEGORY_LABELS,
  COMMON_UNITS,
  isValidCode,
  isValidUnit,
  normalizeUnit,
} from "@/lib/inventory-records";
import { WriteErrorMessage } from "./states";
import { RECORD_KEYS } from "./use-records";

interface MaterialFormProps {
  token: string;
  /** Absent: create a material. Present: edit it (the code never changes). */
  material?: Material;
  /** Prefilled code when registering a code found in consumption. */
  initialCode?: string;
  /** An unlinked stock item to register and link in the same request. */
  linkItem?: { id: string; sku: string };
  onDone: (material: Material) => void;
  onCancel: () => void;
}

export function MaterialForm({ token, material, initialCode, linkItem, onDone, onCancel }: MaterialFormProps) {
  const id = useId();
  const queryClient = useQueryClient();
  const editing = !!material;
  const [code, setCode] = useState(material?.code ?? initialCode ?? "");
  const [name, setName] = useState(material?.name ?? "");
  const [category, setCategory] = useState<MaterialCategory | "">(material?.category ?? "");
  const [baseUnit, setBaseUnit] = useState(material?.base_unit ?? "");
  const [active, setActive] = useState(material?.active ?? true);
  const [notes, setNotes] = useState(material?.notes ?? "");
  const [touched, setTouched] = useState(false);

  const problems: Record<string, string> = {};
  if (!editing && !isValidCode(code))
    problems.code = "De 1 a 64 letras, dígitos, punto, guion o guion bajo; empieza con letra o dígito.";
  if (!name.trim()) problems.name = "Escribe el nombre.";
  if (!category) problems.category = "Elige una categoría.";
  if (!isValidUnit(baseUnit))
    problems.baseUnit = "Minúsculas, dígitos o guion bajo; empieza con letra (g, kg, ml, sheet).";
  const valid = Object.keys(problems).length === 0;

  const mutation = useMutation({
    mutationFn: async () => {
      if (!material) {
        return materialsAPI.create(token, {
          code: code.trim(),
          name: name.trim(),
          category: category as MaterialCategory,
          base_unit: normalizeUnit(baseUnit),
          active,
          notes: notes.trim() || undefined,
          inventory_item_id: linkItem?.id,
        });
      }
      const patch: MaterialPatch = {};
      if (name.trim() !== material.name) patch.name = name.trim();
      if (category !== material.category) patch.category = category as MaterialCategory;
      if (normalizeUnit(baseUnit) !== material.base_unit) patch.base_unit = normalizeUnit(baseUnit);
      if (active !== material.active) patch.active = active;
      if (notes.trim() !== (material.notes ?? "")) patch.notes = notes.trim();
      return materialsAPI.update(token, material.id, patch);
    },
    onSuccess: (saved) => {
      queryClient.invalidateQueries({ queryKey: RECORD_KEYS.materials });
      queryClient.invalidateQueries({ queryKey: RECORD_KEYS.unlinked });
      queryClient.invalidateQueries({ queryKey: RECORD_KEYS.coverage });
      onDone(saved);
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!valid || mutation.isPending) return;
    mutation.mutate();
  };

  const fieldError = (key: string) =>
    touched && problems[key] ? (
      <p id={`${id}-${key}-err`} className="text-xs text-red-700 dark:text-red-400">
        {problems[key]}
      </p>
    ) : null;
  const describedBy = (key: string, hint?: string) =>
    [touched && problems[key] ? `${id}-${key}-err` : null, hint].filter(Boolean).join(" ") || undefined;

  return (
    <form onSubmit={submit} noValidate className="space-y-4" aria-label={editing ? "Editar material" : "Nuevo material"}>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-code`}>Código</Label>
          {editing ? (
            <p id={`${id}-code`} className="font-mono text-sm">
              {material.code}
            </p>
          ) : (
            <Input
              id={`${id}-code`}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
              aria-invalid={touched && !!problems.code}
              aria-describedby={describedBy("code", `${id}-code-hint`)}
            />
          )}
          <p id={`${id}-code-hint`} className="text-xs text-muted-foreground">
            {editing
              ? "El código no se puede cambiar: los registros se refieren al material por él."
              : "Revísalo bien: después no se puede cambiar."}
          </p>
          {fieldError("code")}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-name`}>Nombre</Label>
          <Input
            id={`${id}-name`}
            value={name}
            onChange={(e) => setName(e.target.value)}
            aria-invalid={touched && !!problems.name}
            aria-describedby={describedBy("name")}
          />
          {fieldError("name")}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-category`}>Categoría</Label>
          <Select value={category} onValueChange={(v) => setCategory(v as MaterialCategory)}>
            <SelectTrigger
              id={`${id}-category`}
              aria-invalid={touched && !!problems.category}
              aria-describedby={describedBy("category")}
            >
              <SelectValue placeholder="Elige una categoría" />
            </SelectTrigger>
            <SelectContent>
              {MATERIAL_CATEGORIES.map((c) => (
                <SelectItem key={c} value={c}>
                  {CATEGORY_LABELS[c]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {fieldError("category")}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-unit`}>Unidad base</Label>
          <Input
            id={`${id}-unit`}
            value={baseUnit}
            onChange={(e) => setBaseUnit(e.target.value)}
            list={`${id}-units`}
            autoComplete="off"
            spellCheck={false}
            className="font-mono"
            aria-invalid={touched && !!problems.baseUnit}
            aria-describedby={describedBy("baseUnit", `${id}-unit-hint`)}
          />
          <datalist id={`${id}-units`}>
            {COMMON_UNITS.map((u) => (
              <option key={u} value={u} />
            ))}
          </datalist>
          <p id={`${id}-unit-hint`} className="text-xs text-muted-foreground">
            La unidad en que se mide normalmente (g para filamento, ml para resina).
          </p>
          {fieldError("baseUnit")}
        </div>
      </div>
      {linkItem && !editing && (
        <p className="rounded-md border bg-muted/50 p-3 text-sm">
          Se vinculará al artículo de existencias <span className="font-mono">{linkItem.sku}</span> en el
          mismo registro.
        </p>
      )}
      <div className="space-y-1.5">
        <Label htmlFor={`${id}-notes`}>Notas (opcional)</Label>
        <Textarea id={`${id}-notes`} value={notes} onChange={(e) => setNotes(e.target.value)} rows={2} />
      </div>
      <div className="flex items-start gap-2">
        <Checkbox
          id={`${id}-active`}
          checked={active}
          onCheckedChange={(v) => setActive(v === true)}
          className="mt-0.5"
        />
        <div>
          <Label htmlFor={`${id}-active`}>Activo</Label>
          <p className="text-xs text-muted-foreground">
            Un material inactivo no acepta entradas nuevas; su historial se conserva.
          </p>
        </div>
      </div>
      <WriteErrorMessage error={mutation.error} forbiddenAction="cambiar el registro de materiales" />
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? "Guardando…" : editing ? "Guardar cambios" : "Registrar material"}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel} disabled={mutation.isPending}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}
