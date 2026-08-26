package services

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// newMiniredisPublisher builds a real Publisher backed by miniredis; attach
// an outbox sink with EnableOutbox to share the test's sqlmock database so
// SQL expectations stay ordered and deterministic.
func newMiniredisPublisher(t *testing.T) (*pubsub.Publisher, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)

	pub, err := pubsub.NewPublisher(pubsub.PublisherConfig{RedisURL: "redis://" + mr.Addr()}, quietLog())
	require.NoError(t, err)

	cleanup := func() {
		pub.Close()
		mr.Close()
	}
	return pub, cleanup
}

func testOrder(tenantID uuid.UUID) *types.Order {
	return &types.Order{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ExternalID:   "COT-1001",
		CustomerName: "Acme MX",
		Status:       types.OrderStatusReceived,
		Priority:     7,
	}
}

func TestDecomposeOrder_CreatesOneTaskPerItem(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	order := testOrder(tenantID)
	items := []types.OrderItem{
		{
			ID:          uuid.New(),
			OrderID:     order.ID,
			ProductName: "Bracket v2",
			Quantity:    3,
			Specifications: map[string]any{
				"process":  "3d_printing",
				"material": "pla",
			},
			CADFileURL: "https://r2.example/bracket.stl",
		},
		{
			ID:          uuid.New(),
			OrderID:     order.ID,
			ProductName: "Cover plate",
			Quantity:    1,
		},
	}

	// One INSERT INTO tasks per item.
	now := time.Now()
	mock.ExpectQuery("INSERT INTO tasks").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))
	mock.ExpectQuery("INSERT INTO tasks").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	// Auto-assign disabled, no publisher: pure decomposition.
	svc := NewOrderDecompositionService(repositories.NewTaskRepository(db), nil, nil, false, quietLog())

	tasks := svc.DecomposeOrder(context.Background(), order, items, uuid.New())

	require.Len(t, tasks, 2, "one task per order item")

	first := tasks[0]
	assert.Equal(t, "Bracket v2 ×3", first.Title, "title comes from product_name and quantity")
	assert.Equal(t, types.TaskStatusBacklog, first.Status, "tasks land in the existing backlog column")
	require.NotNil(t, first.OrderID)
	assert.Equal(t, order.ID, *first.OrderID, "task links its order")
	require.NotNil(t, first.OrderItemID)
	assert.Equal(t, items[0].ID, *first.OrderItemID, "task links its order item")
	assert.Nil(t, first.MachineID, "auto-assign disabled leaves the task unassigned")
	assert.Equal(t, order.Priority, first.Priority, "priority carried from the order")

	// Specifications + CAD file carried forward into task metadata.
	assert.Equal(t, items[0].Specifications, first.Metadata["specifications"])
	assert.Equal(t, "https://r2.example/bracket.stl", first.Metadata["cad_file_url"])
	assert.Equal(t, "order_decomposition", first.Metadata["source"])

	second := tasks[1]
	assert.Equal(t, "Cover plate", second.Title, "quantity 1 keeps the bare product name")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDecomposeOrder_AutoAssignSetsMachine(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	order := testOrder(tenantID)
	items := []types.OrderItem{{
		ID:          uuid.New(),
		OrderID:     order.ID,
		ProductName: "Bracket v2",
		Quantity:    2,
		Specifications: map[string]any{
			"process": "3d_printing",
		},
	}}

	// 1) assignment candidates query finds a matching idle printer
	rows := sqlmock.NewRows(candidateColumns)
	candidateRow(rows, tenantID, "printer-1", "idle", `["3d_printing"]`, 0, nil)
	mock.ExpectQuery("FROM machines m").WithArgs(tenantID).WillReturnRows(rows)

	// 2) task insert
	now := time.Now()
	mock.ExpectQuery("INSERT INTO tasks").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	assignment := NewMachineAssignmentService(repositories.NewMachineRepository(db), nil, quietLog())
	svc := NewOrderDecompositionService(repositories.NewTaskRepository(db), assignment, nil, true, quietLog())

	tasks := svc.DecomposeOrder(context.Background(), order, items, uuid.New())

	require.Len(t, tasks, 1)
	require.NotNil(t, tasks[0].MachineID, "matching machine must be assigned at creation")
	assert.Equal(t, "capability_match", tasks[0].Metadata["assignment_basis"])
	assert.Equal(t, "printer-1", tasks[0].Metadata["assigned_machine_code"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDecomposeOrder_NoMachineMatchFailsVisible(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub, cleanup := newMiniredisPublisher(t)
	defer cleanup()
	pub.EnableOutbox(repositories.NewOutboxRepository(db))

	tenantID := uuid.New()
	order := testOrder(tenantID)
	items := []types.OrderItem{{
		ID:          uuid.New(),
		OrderID:     order.ID,
		ProductName: "Titanium frame",
		Quantity:    1,
		Specifications: map[string]any{
			"process":  "cnc_milling",
			"material": "titanium",
		},
	}}

	// 1) candidates exist but none satisfies the requirements
	rows := sqlmock.NewRows(candidateColumns)
	candidateRow(rows, tenantID, "printer-1", "idle", `["3d_printing"]`, 0, nil)
	mock.ExpectQuery("FROM machines m").WithArgs(tenantID).WillReturnRows(rows)

	// 2) the task is still created — unassigned
	now := time.Now()
	mock.ExpectQuery("INSERT INTO tasks").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	// 3) fail VISIBLE: task.assignment_failed lands in the outbox...
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(sqlmock.AnyArg(), tenantID, string(pubsub.EventTaskAssignmentFailed), string(pubsub.NamespaceTasks), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	// 4) ...plus a warning notification for the UI
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(sqlmock.AnyArg(), tenantID, string(pubsub.EventNotificationWarning), string(pubsub.NamespaceNotifications), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	// 5) and the task.created event
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(sqlmock.AnyArg(), tenantID, string(pubsub.EventTaskCreated), string(pubsub.NamespaceTasks), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))

	assignment := NewMachineAssignmentService(repositories.NewMachineRepository(db), pub, quietLog())
	svc := NewOrderDecompositionService(repositories.NewTaskRepository(db), assignment, pub, true, quietLog())

	tasks := svc.DecomposeOrder(context.Background(), order, items, uuid.New())

	require.Len(t, tasks, 1)
	assert.Nil(t, tasks[0].MachineID, "unmatched task stays unassigned (pending human)")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDecomposeOrder_NoRequirementsSkipsAssignmentQuietly(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	order := testOrder(tenantID)
	items := []types.OrderItem{{
		ID:          uuid.New(),
		OrderID:     order.ID,
		ProductName: "Mystery widget",
		Quantity:    1,
		// No specifications: no capability requirements.
	}}

	// Only the task insert — no machine query, no assignment_failed event.
	now := time.Now()
	mock.ExpectQuery("INSERT INTO tasks").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	assignment := NewMachineAssignmentService(repositories.NewMachineRepository(db), nil, quietLog())
	svc := NewOrderDecompositionService(repositories.NewTaskRepository(db), assignment, nil, true, quietLog())

	tasks := svc.DecomposeOrder(context.Background(), order, items, uuid.New())

	require.Len(t, tasks, 1)
	assert.Nil(t, tasks[0].MachineID, "no requirements -> left for human routing, not a failure")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskTitleForItem(t *testing.T) {
	assert.Equal(t, "Bracket ×4", taskTitleForItem(&types.OrderItem{ProductName: "Bracket", Quantity: 4}))
	assert.Equal(t, "Bracket", taskTitleForItem(&types.OrderItem{ProductName: "Bracket", Quantity: 1}))
}
