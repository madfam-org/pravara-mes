package services

import (
	"context"
	"fmt"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// builtinConversions are exact metric conversions every material shares
// (1 from = factor to). Anything else (spool → g, mm of filament → g,
// sheet → pcs) is per material in material_unit_conversions; nothing is
// ever guessed.
var builtinConversions = map[[2]string]float64{
	{"kg", "g"}: 1000, {"g", "kg"}: 0.001,
	{"l", "ml"}: 1000, {"ml", "l"}: 0.001,
	{"m", "mm"}: 1000, {"mm", "m"}: 0.001,
	{"cm", "mm"}: 10, {"mm", "cm"}: 0.1,
}

// BuiltinConversion returns the built-in factor between two normalized
// units (1 for the same unit).
func BuiltinConversion(from, to string) (float64, bool) {
	if from == to {
		return 1, true
	}
	f, ok := builtinConversions[[2]string{from, to}]
	return f, ok
}

// stockFactor is the factor from a quantity's unit to item's unit: the same
// unit, a built-in metric conversion, or the material's own conversion.
// ok is false when none exists: the caller records a conversion gap and
// leaves stock untouched.
func stockFactor(ctx context.Context, stock *repositories.StockRepository, item *repositories.StockItem, unit string) (float64, bool, error) {
	from, to := repositories.NormalizeUnit(unit), repositories.NormalizeUnit(item.Unit)
	if f, ok := BuiltinConversion(from, to); ok {
		return f, true, nil
	}
	if item.MaterialID == nil {
		return 0, false, nil
	}
	f, ok, err := stock.MaterialConversion(ctx, *item.MaterialID, from, to)
	if err != nil {
		return 0, false, fmt.Errorf("unit conversion %s → %s: %w", from, to, err)
	}
	return f, ok, nil
}
