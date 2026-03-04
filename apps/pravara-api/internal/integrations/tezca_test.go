package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchLaws(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/laws/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("search") != "constitución" {
			t.Errorf("expected search=constitución, got %s", r.URL.Query().Get("search"))
		}
		if r.Header.Get("X-API-Key") != "tzk_test" {
			t.Errorf("expected X-API-Key=tzk_test, got %s", r.Header.Get("X-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}))
	defer srv.Close()

	client := NewTezcaClient(srv.URL, "tzk_test")
	result, err := client.SearchLaws(context.Background(), "constitución", "")
	if err != nil {
		t.Fatalf("SearchLaws error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestSearchArticles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "impuesto" {
			t.Errorf("expected q=impuesto, got %s", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("domain") != "manufacturing" {
			t.Errorf("expected domain=manufacturing, got %s", r.URL.Query().Get("domain"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}))
	defer srv.Close()

	client := NewTezcaClient(srv.URL, "tzk_test")
	_, err := client.SearchArticles(context.Background(), "impuesto", "manufacturing")
	if err != nil {
		t.Fatalf("SearchArticles error: %v", err)
	}
}

func TestGetLawDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/laws/cff/" {
			t.Errorf("unexpected path: %s, expected /laws/cff/", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "tzk_pravara" {
			t.Errorf("missing or wrong API key header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"official_id": "cff", "name": "CFF"})
	}))
	defer srv.Close()

	client := NewTezcaClient(srv.URL, "tzk_pravara")
	result, err := client.GetLawDetail(context.Background(), "cff")
	if err != nil {
		t.Fatalf("GetLawDetail error: %v", err)
	}
	if result["official_id"] != "cff" {
		t.Errorf("expected official_id=cff, got %v", result["official_id"])
	}
}

func TestGetLawArticles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/laws/cff/articles/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") != "2" {
			t.Errorf("expected page=2, got %s", r.URL.Query().Get("page"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}))
	defer srv.Close()

	client := NewTezcaClient(srv.URL, "tzk_test")
	_, err := client.GetLawArticles(context.Background(), "cff", 2)
	if err != nil {
		t.Fatalf("GetLawArticles error: %v", err)
	}
}

func TestGetChangelog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/changelog/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("since") != "2026-01-01" {
			t.Errorf("expected since=2026-01-01, got %s", r.URL.Query().Get("since"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"changes": []interface{}{}})
	}))
	defer srv.Close()

	client := NewTezcaClient(srv.URL, "tzk_test")
	_, err := client.GetChangelog(context.Background(), "2026-01-01")
	if err != nil {
		t.Fatalf("GetChangelog error: %v", err)
	}
}

func TestHTTPErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"not found"}`))
	}))
	defer srv.Close()

	client := NewTezcaClient(srv.URL, "tzk_test")
	_, err := client.GetLawDetail(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
