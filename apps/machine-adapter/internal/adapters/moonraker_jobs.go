package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// materialRefresh bounds how often Moonraker is asked for the loaded filament.
const materialRefresh = 15 * time.Second

// AcceptsMediaType implements JobRunner: Klipper runs G-code.
func (a *MoonrakerAdapter) AcceptsMediaType(mt string) bool { return mt == MediaTypeGCode }

// UploadFile implements JobRunner via POST /server/files/upload (root "gcodes").
func (a *MoonrakerAdapter) UploadFile(ctx context.Context, name string, r io.Reader, _ int64) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := mw.WriteField("root", "gcodes")
		if err == nil {
			var part io.Writer
			part, err = mw.CreateFormFile("file", name)
			if err == nil {
				_, err = io.Copy(part, r)
			}
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/server/files/upload", pr)
	if err != nil {
		pr.CloseWithError(err)
		return fmt.Errorf("upload request: %w", err)
	}
	a.setHeaders(req)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := *a.httpClient
	client.Timeout = 0 // large files: bounded by ctx instead
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("upload error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// StartPrint implements JobRunner via POST /printer/print/start.
func (a *MoonrakerAdapter) StartPrint(ctx context.Context, name string) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}
	timeout := 30 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}
	return a.postAPI("/printer/print/start?filename="+url.QueryEscape(name), nil, timeout)
}

// Snapshot implements SnapshotSource from /printer/objects/query.
func (a *MoonrakerAdapter) Snapshot(ctx context.Context) (PrinterSnapshot, error) {
	var out struct {
		Result struct {
			Status map[string]map[string]interface{} `json:"status"`
		} `json:"result"`
	}
	if err := a.getJSON(ctx, "/printer/objects/query?webhooks&heater_bed&extruder&print_stats&display_status", &out); err != nil {
		return PrinterSnapshot{}, err
	}
	st := out.Result.Status
	a.updateStatusFromQuery(st)
	s := a.GetStatus()

	snap := PrinterSnapshot{
		HotendC:  s.ExtruderTemp,
		BedC:     s.BedTemp,
		Progress: clampPercent(s.Progress * 100),
		JobFile:  s.Filename,
	}
	klippy, _ := st["webhooks"]["state"].(string)
	snap.Status, snap.JobState = moonrakerStates(klippy, s.PrintState)
	snap.Materials = a.loadedMaterials(ctx, s.Filename)
	return snap, nil
}

// moonrakerStates maps Klippy's webhooks.state and print_stats.state.
func moonrakerStates(klippy, printState string) (string, JobState) {
	switch klippy {
	case "ready", "":
	case "startup":
		return PrinterOffline, JobStateNone
	default: // shutdown, error
		return PrinterError, JobStateNone
	}
	switch strings.ToLower(printState) {
	case "printing":
		return PrinterPrinting, JobStatePrinting
	case "paused":
		return PrinterPaused, JobStatePaused
	case "complete":
		return PrinterIdle, JobStateComplete
	case "cancelled":
		return PrinterIdle, JobStateCancelled
	case "error":
		return PrinterError, JobStateFailed
	default: // standby
		return PrinterIdle, JobStateNone
	}
}

// loadedMaterials reports slot 1 (single toolhead): the Spoolman active spool's
// material when the Spoolman component is configured, otherwise the
// filament_type in the current file's metadata. Results are cached briefly.
func (a *MoonrakerAdapter) loadedMaterials(ctx context.Context, file string) []LoadedMaterial {
	a.mu.RLock()
	cached, at := a.materials, a.materialsAt
	a.mu.RUnlock()
	if cached != nil && time.Since(at) < materialRefresh {
		return cached
	}
	slot := LoadedMaterial{}
	if m := a.spoolmanMaterial(ctx); m != "" {
		slot = LoadedMaterial{Vendor: m, Loaded: true}
	} else if file != "" {
		var meta struct {
			Result struct {
				FilamentType string `json:"filament_type"`
			} `json:"result"`
		}
		if err := a.getJSON(ctx, "/server/files/metadata?filename="+url.QueryEscape(file), &meta); err == nil {
			ft := strings.TrimSpace(strings.Split(meta.Result.FilamentType, ";")[0])
			slot = LoadedMaterial{Vendor: ft, Loaded: ft != ""}
		}
	}
	out := []LoadedMaterial{slot}
	a.mu.Lock()
	a.materials, a.materialsAt = out, time.Now()
	a.mu.Unlock()
	return out
}

// spoolmanMaterial returns the active spool's filament material, or "".
func (a *MoonrakerAdapter) spoolmanMaterial(ctx context.Context) string {
	var id struct {
		Result struct {
			SpoolID *int `json:"spool_id"`
		} `json:"result"`
	}
	if err := a.getJSON(ctx, "/server/spoolman/spool_id", &id); err != nil || id.Result.SpoolID == nil {
		return ""
	}
	body, _ := json.Marshal(map[string]string{
		"request_method": "GET",
		"path":           fmt.Sprintf("/v1/spool/%d", *id.Result.SpoolID),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/server/spoolman/proxy", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	a.setHeaders(req)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var spool struct {
		Result struct {
			Filament struct {
				Material string `json:"material"`
			} `json:"filament"`
		} `json:"result"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&spool) != nil {
		return ""
	}
	return strings.TrimSpace(spool.Result.Filament.Material)
}

// getJSON performs an authenticated GET and decodes the JSON body.
func (a *MoonrakerAdapter) getJSON(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path, nil)
	if err != nil {
		return err
	}
	a.setHeaders(req)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", strings.SplitN(path, "?", 2)[0], err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", strings.SplitN(path, "?", 2)[0], resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	}
	return v
}
