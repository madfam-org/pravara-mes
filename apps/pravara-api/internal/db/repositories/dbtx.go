package repositories

import "github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"

// DBTX is the handle repositories issue statements on. In production it is a
// *db.TenantDB, which runs every statement inside the caller's tenant-scoped
// transaction; unit tests pass a *sql.DB (sqlmock).
type DBTX = db.Querier

// tenantMatch is a defence-in-depth predicate added to tenant-table reads,
// updates and deletes, on top of row-level security. It compares against the
// transaction-local tenant set by internal/db and matches nothing when no
// tenant is set, so it holds even for a connection role that is exempt from
// RLS. Use tenantMatchOn for aliased tables.
const tenantMatch = "tenant_id = NULLIF(current_setting('app.current_tenant_id', true), '')::uuid"

// tenantMatchOn returns tenantMatch qualified with a table alias.
func tenantMatchOn(alias string) string {
	return alias + "." + tenantMatch
}

// orderItemTenantMatch scopes order_items, which carry no tenant_id, through
// their order.
const orderItemTenantMatch = "order_id IN (SELECT id FROM orders WHERE " + tenantMatch + ")"

// deliveryTenantMatch scopes webhook_deliveries, which carry no tenant_id,
// through their subscription.
const deliveryTenantMatch = "subscription_id IN (SELECT id FROM webhook_subscriptions WHERE " + tenantMatch + ")"
