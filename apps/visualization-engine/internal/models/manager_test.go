package models

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// ---------------------------------------------------------------------------
// Fake SQL driver for testing without external dependencies (no sqlmock).
//
// This provides a minimal *sql.DB that returns controlled results for
// Manager methods. Each test configures fakeConnector with the rows/errors
// it needs.
//
// NOTE: For production-grade integration tests, consider adding
// DATA-DOG/go-sqlmock to go.mod. This lightweight approach tests the
// Manager logic without requiring any external packages.
// ---------------------------------------------------------------------------

// fakeDriver / fakeConn / fakeStmt / fakeRows implement the minimum
// driver interfaces needed by database/sql to open and query.

type fakeConnector struct {
	queryFunc func(query string, args []driver.Value) (driver.Rows, error)
	execFunc  func(query string, args []driver.Value) (driver.Result, error)
}

func (fc *fakeConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return &fakeConn{connector: fc}, nil
}

func (fc *fakeConnector) Driver() driver.Driver { return nil }

type fakeConn struct {
	connector *fakeConnector
}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{query: query, conn: c}, nil
}

func (c *fakeConn) Close() error                 { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)     { return &fakeTx{}, nil }

type fakeTx struct{}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

type fakeStmt struct {
	query string
	conn  *fakeConn
}

func (s *fakeStmt) Close() error                               { return nil }
func (s *fakeStmt) NumInput() int                               { return -1 }
func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	if s.conn.connector.execFunc != nil {
		return s.conn.connector.execFunc(s.query, args)
	}
	return &fakeResult{}, nil
}
func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	if s.conn.connector.queryFunc != nil {
		return s.conn.connector.queryFunc(s.query, args)
	}
	return &fakeRows{closed: true}, nil
}

type fakeResult struct {
	lastID   int64
	affected int64
}

func (r *fakeResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r *fakeResult) RowsAffected() (int64, error) { return r.affected, nil }

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
	closed  bool
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { r.closed = true; return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return fmt.Errorf("EOF")
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestLogger() *logrus.Logger {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)
	return log
}

func newTestDB(connector *fakeConnector) *sql.DB {
	return sql.OpenDB(connector)
}

func modelColumns() []string {
	return []string{
		"id", "machine_type", "name", "model_url", "thumbnail_url",
		"bounding_box", "origin_offset", "scale", "lod_levels",
		"materials", "animations", "created_at", "updated_at",
	}
}

func sampleModelRow(id uuid.UUID) []driver.Value {
	bb, _ := json.Marshal(BoundingBox{
		Min: Vector3{0, 0, 0}, Max: Vector3{1, 1, 1},
	})
	origin, _ := json.Marshal(Vector3{0, 0, 0})
	lod, _ := json.Marshal([]LODLevel{})
	mats, _ := json.Marshal([]Material{})
	anims, _ := json.Marshal([]Animation{})
	now := time.Now()

	return []driver.Value{
		id.String(),
		"CNC_3axis",
		"Test Mill",
		"https://example.com/model.gltf",
		"https://example.com/thumb.png",
		bb,
		origin,
		1.0,
		lod,
		mats,
		anims,
		now,
		now,
	}
}

// ---------------------------------------------------------------------------
// GetModel tests
// ---------------------------------------------------------------------------

func TestGetModel_ValidID(t *testing.T) {
	modelID := uuid.New()

	connector := &fakeConnector{
		queryFunc: func(query string, args []driver.Value) (driver.Rows, error) {
			if !strings.Contains(query, "WHERE id") {
				return &fakeRows{closed: true}, nil
			}
			return &fakeRows{
				columns: modelColumns(),
				rows:    [][]driver.Value{sampleModelRow(modelID)},
			}, nil
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	model, err := mgr.GetModel(context.Background(), modelID.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}
	if model.Name != "Test Mill" {
		t.Errorf("model name = %q, want 'Test Mill'", model.Name)
	}
	if model.MachineType != "CNC_3axis" {
		t.Errorf("machine type = %q, want 'CNC_3axis'", model.MachineType)
	}
}

func TestGetModel_InvalidUUID(t *testing.T) {
	connector := &fakeConnector{}
	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	_, err := mgr.GetModel(context.Background(), "not-a-uuid")
	if err == nil {
		t.Error("expected error for invalid UUID")
	}
	if !strings.Contains(err.Error(), "invalid model ID") {
		t.Errorf("error = %q, want to contain 'invalid model ID'", err.Error())
	}
}

func TestGetModel_NotFound(t *testing.T) {
	connector := &fakeConnector{
		queryFunc: func(query string, args []driver.Value) (driver.Rows, error) {
			// Return empty result set by returning EOF immediately
			return &fakeRows{columns: modelColumns(), rows: [][]driver.Value{}, closed: false}, nil
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	_, err := mgr.GetModel(context.Background(), uuid.New().String())

	// QueryRow.Scan should return sql.ErrNoRows which maps to "model not found"
	if err == nil {
		t.Error("expected error for missing model")
	}
}

func TestGetModel_DBError(t *testing.T) {
	connector := &fakeConnector{
		queryFunc: func(query string, args []driver.Value) (driver.Rows, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	_, err := mgr.GetModel(context.Background(), uuid.New().String())
	if err == nil {
		t.Error("expected error on DB failure")
	}
}

// ---------------------------------------------------------------------------
// CreateModel tests
// ---------------------------------------------------------------------------

func TestCreateModel_SetsIDAndTimestamps(t *testing.T) {
	connector := &fakeConnector{
		execFunc: func(query string, args []driver.Value) (driver.Result, error) {
			return &fakeResult{affected: 1}, nil
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	model := &MachineModel{
		MachineType: "CNC_5axis",
		Name:        "New Machine",
		ModelURL:    "https://example.com/new.gltf",
		Scale:       1.0,
	}

	before := time.Now()
	err := mgr.CreateModel(context.Background(), model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if model.ID == uuid.Nil {
		t.Error("model ID should be set after creation")
	}
	if model.CreatedAt.Before(before) {
		t.Error("CreatedAt should be set to current time")
	}
	if model.UpdatedAt.Before(before) {
		t.Error("UpdatedAt should be set to current time")
	}
}

func TestCreateModel_DBError(t *testing.T) {
	connector := &fakeConnector{
		execFunc: func(query string, args []driver.Value) (driver.Result, error) {
			return nil, fmt.Errorf("duplicate key violation")
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	model := &MachineModel{Name: "Duplicate"}
	err := mgr.CreateModel(context.Background(), model)
	if err == nil {
		t.Error("expected error on DB failure")
	}
	if !strings.Contains(err.Error(), "failed to create model") {
		t.Errorf("error = %q, want to contain 'failed to create model'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// UpdateModel tests
// ---------------------------------------------------------------------------

func TestUpdateModel_InvalidUUID(t *testing.T) {
	connector := &fakeConnector{}
	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	err := mgr.UpdateModel(context.Background(), "bad-uuid", &MachineModel{})
	if err == nil {
		t.Error("expected error for invalid UUID")
	}
	if !strings.Contains(err.Error(), "invalid model ID") {
		t.Errorf("error = %q, want 'invalid model ID'", err.Error())
	}
}

func TestUpdateModel_NotFound(t *testing.T) {
	connector := &fakeConnector{
		execFunc: func(query string, args []driver.Value) (driver.Result, error) {
			return &fakeResult{affected: 0}, nil
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	err := mgr.UpdateModel(context.Background(), uuid.New().String(), &MachineModel{Name: "Ghost"})
	if err == nil {
		t.Error("expected 'model not found' error")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %q, want 'model not found'", err.Error())
	}
}

func TestUpdateModel_Success(t *testing.T) {
	connector := &fakeConnector{
		execFunc: func(query string, args []driver.Value) (driver.Result, error) {
			return &fakeResult{affected: 1}, nil
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	model := &MachineModel{Name: "Updated Name"}
	before := time.Now()
	err := mgr.UpdateModel(context.Background(), uuid.New().String(), model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.UpdatedAt.Before(before) {
		t.Error("UpdatedAt should be refreshed")
	}
}

func TestUpdateModel_DBError(t *testing.T) {
	connector := &fakeConnector{
		execFunc: func(query string, args []driver.Value) (driver.Result, error) {
			return nil, fmt.Errorf("deadlock detected")
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	err := mgr.UpdateModel(context.Background(), uuid.New().String(), &MachineModel{})
	if err == nil {
		t.Error("expected error on DB failure")
	}
}

// ---------------------------------------------------------------------------
// DeleteModel tests
// ---------------------------------------------------------------------------

func TestDeleteModel_InvalidUUID(t *testing.T) {
	connector := &fakeConnector{}
	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	err := mgr.DeleteModel(context.Background(), "xyz")
	if err == nil {
		t.Error("expected error for invalid UUID")
	}
}

func TestDeleteModel_NotFound(t *testing.T) {
	connector := &fakeConnector{
		execFunc: func(query string, args []driver.Value) (driver.Result, error) {
			return &fakeResult{affected: 0}, nil
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	err := mgr.DeleteModel(context.Background(), uuid.New().String())
	if err == nil {
		t.Error("expected 'model not found' error")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %q, want 'model not found'", err.Error())
	}
}

func TestDeleteModel_Success(t *testing.T) {
	connector := &fakeConnector{
		execFunc: func(query string, args []driver.Value) (driver.Result, error) {
			return &fakeResult{affected: 1}, nil
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	err := mgr.DeleteModel(context.Background(), uuid.New().String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteModel_DBError(t *testing.T) {
	connector := &fakeConnector{
		execFunc: func(query string, args []driver.Value) (driver.Result, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	err := mgr.DeleteModel(context.Background(), uuid.New().String())
	if err == nil {
		t.Error("expected error on DB failure")
	}
}

// ---------------------------------------------------------------------------
// ListModels tests
// ---------------------------------------------------------------------------

func TestListModels_Empty(t *testing.T) {
	connector := &fakeConnector{
		queryFunc: func(query string, args []driver.Value) (driver.Rows, error) {
			return &fakeRows{columns: modelColumns(), rows: [][]driver.Value{}}, nil
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	models, err := mgr.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestListModels_DBError(t *testing.T) {
	connector := &fakeConnector{
		queryFunc: func(query string, args []driver.Value) (driver.Rows, error) {
			return nil, fmt.Errorf("table does not exist")
		},
	}

	db := newTestDB(connector)
	defer db.Close()

	mgr := NewManager(db, newTestLogger())
	_, err := mgr.ListModels(context.Background())
	if err == nil {
		t.Error("expected error on DB failure")
	}
}

// ---------------------------------------------------------------------------
// Data type unit tests
// ---------------------------------------------------------------------------

func TestMachineModel_JSONRoundTrip(t *testing.T) {
	model := MachineModel{
		ID:          uuid.New(),
		MachineType: "3dprinter",
		Name:        "Prusa MK4",
		ModelURL:    "https://cdn.example.com/prusa.gltf",
		Scale:       1.5,
		BoundingBox: BoundingBox{
			Min: Vector3{0, 0, 0},
			Max: Vector3{25, 21, 21},
		},
		LODLevels: []LODLevel{
			{Distance: 10, ModelURL: "low.gltf", VertexCount: 500},
		},
		Materials: []Material{
			{Name: "body", Color: "#333333", Metalness: 0.8, Roughness: 0.3, Opacity: 1.0},
		},
		Animations: []Animation{
			{Name: "print", Duration: 5.0, Loop: true, Type: "operation"},
		},
	}

	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded MachineModel
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Name != model.Name {
		t.Errorf("name = %q, want %q", decoded.Name, model.Name)
	}
	if decoded.Scale != model.Scale {
		t.Errorf("scale = %v, want %v", decoded.Scale, model.Scale)
	}
	if len(decoded.LODLevels) != 1 {
		t.Errorf("LOD levels = %d, want 1", len(decoded.LODLevels))
	}
	if len(decoded.Materials) != 1 {
		t.Errorf("materials = %d, want 1", len(decoded.Materials))
	}
}

func TestNewManager(t *testing.T) {
	connector := &fakeConnector{}
	db := newTestDB(connector)
	defer db.Close()

	log := newTestLogger()
	mgr := NewManager(db, log)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
}
