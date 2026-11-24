package repositories

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Ref names a tenant table whose rows a write may reference by id.
type Ref string

// Tables a request body may reference.
const (
	RefMachines            Ref = "machines"
	RefUsers               Ref = "users"
	RefOrders              Ref = "orders"
	RefOrderItems          Ref = "order_items"
	RefTasks               Ref = "tasks"
	RefMaintenanceSchedule Ref = "maintenance_schedules"
	RefBatchLots           Ref = "batch_lots"
	RefCertificates        Ref = "quality_certificates"
	RefInspections         Ref = "inspections"
	RefProductDefinitions  Ref = "product_definitions"
)

// refExistsQueries holds one fixed statement per Ref: the row must exist and
// belong to the tenant of the current transaction (tenantMatch, or through
// the parent order for order_items).
var refExistsQueries = map[Ref]string{
	RefMachines:            `SELECT EXISTS (SELECT 1 FROM machines WHERE id = $1 AND ` + tenantMatch + `)`,
	RefUsers:               `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND ` + tenantMatch + `)`,
	RefOrders:              `SELECT EXISTS (SELECT 1 FROM orders WHERE id = $1 AND ` + tenantMatch + `)`,
	RefOrderItems:          `SELECT EXISTS (SELECT 1 FROM order_items WHERE id = $1 AND ` + orderItemTenantMatch + `)`,
	RefTasks:               `SELECT EXISTS (SELECT 1 FROM tasks WHERE id = $1 AND ` + tenantMatch + `)`,
	RefMaintenanceSchedule: `SELECT EXISTS (SELECT 1 FROM maintenance_schedules WHERE id = $1 AND ` + tenantMatch + `)`,
	RefBatchLots:           `SELECT EXISTS (SELECT 1 FROM batch_lots WHERE id = $1 AND ` + tenantMatch + `)`,
	RefCertificates:        `SELECT EXISTS (SELECT 1 FROM quality_certificates WHERE id = $1 AND ` + tenantMatch + `)`,
	RefInspections:         `SELECT EXISTS (SELECT 1 FROM inspections WHERE id = $1 AND ` + tenantMatch + `)`,
	RefProductDefinitions:  `SELECT EXISTS (SELECT 1 FROM product_definitions WHERE id = $1 AND ` + tenantMatch + `)`,
}

// ReferenceExists reports whether id names a row of ref in the current
// tenant. A row of another tenant reads as absent.
func ReferenceExists(ctx context.Context, db DBTX, ref Ref, id uuid.UUID) (bool, error) {
	query, ok := refExistsQueries[ref]
	if !ok {
		return false, fmt.Errorf("unknown reference table %q", ref)
	}
	var exists bool
	if err := db.QueryRowContext(ctx, query, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to look up %s %s: %w", ref, id, err)
	}
	return exists, nil
}
