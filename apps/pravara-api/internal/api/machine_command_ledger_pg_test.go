package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// Runs only with PRAVARA_TEST_DATABASE_URL (a throwaway database).
func openCommandTestDB(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("PRAVARA_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("PRAVARA_TEST_DATABASE_URL not set; skipping PostgreSQL test")
	}
	db, err := sql.Open("postgres", raw)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })

	var exists bool
	require.NoError(t, db.QueryRow(`SELECT to_regclass('public.task_commands') IS NOT NULL`).Scan(&exists))
	if !exists {
		files, _ := filepath.Glob(filepath.Join("..", "db", "migrations", "*.up.sql"))
		sort.Strings(files)
		for _, f := range files {
			body, err := os.ReadFile(f)
			require.NoError(t, err)
			_, err = db.Exec(string(body))
			require.NoError(t, err, f)
		}
	}
	return db
}

func sendDirectCommand(t *testing.T, h *MachineHandler, machineID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/machines/:id/command", h.SendCommand)
	body, _ := json.Marshal(map[string]interface{}{"command": "home"})
	req := httptest.NewRequest(http.MethodPost, "/machines/"+machineID.String()+"/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSendCommandPG_RecordsLedgerRowAndEnqueues(t *testing.T) {
	db := openCommandTestDB(t)
	tenantID, machineID := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'T', $2)`, tenantID, "t-"+tenantID.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tenants WHERE id = $1`, tenantID) })
	_, err = db.Exec(`INSERT INTO machines (id, tenant_id, name, code, type, status, mqtt_topic)
		VALUES ($1, $2, 'Sim', 'sim', '3d_printer', 'online', 'org/site/area/line/sim')`, machineID, tenantID)
	require.NoError(t, err)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	pub, err := pubsub.NewPublisher(pubsub.PublisherConfig{RedisURL: "redis://" + mr.Addr()}, log)
	require.NoError(t, err)
	defer func() { _ = pub.Close() }()

	h := NewMachineHandler(repositories.NewMachineRepository(db), repositories.NewTelemetryRepository(db), log)
	h.SetCommandLedger(repositories.NewTaskCommandRepository(db))

	// No publisher: refused, nothing recorded.
	w := sendDirectCommand(t, h, machineID)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	h.SetPublisher(pub)
	w = sendDirectCommand(t, h, machineID)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var resp struct {
		CommandID uuid.UUID `json:"command_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	var status string
	var taskID sql.NullString
	require.NoError(t, db.QueryRow(`SELECT status, task_id FROM task_commands WHERE command_id = $1`, resp.CommandID).Scan(&status, &taskID))
	require.Equal(t, "pending", status)
	require.False(t, taskID.Valid)

	entries, err := mr.Stream(pubsub.DefaultCommandStreamKey)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Stream unavailable: the ledger row is failed, not left pending.
	mr.Close()
	w = sendDirectCommand(t, h, machineID)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	var failed int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM task_commands WHERE machine_id = $1 AND status = 'failed'
		AND error_message LIKE 'Failed to enqueue:%'`, machineID).Scan(&failed))
	require.Equal(t, 1, failed)
}
