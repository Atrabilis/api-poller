package poller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Atrabilis/api-poller/internal/config"
)

func TestRunInputJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"io":{"ai":[{"aiIndex":0,"aiValueScaled":1.5}]}}`))
	}))
	defer server.Close()

	cfg := config.Config{
		Inputs: []config.Input{{
			Name: "json_input",
			Request: config.Request{
				Method:  "GET",
				URL:     server.URL,
				Timeout: 5 * time.Second,
			},
			Response: config.Response{
				Format:    "json",
				ItemsPath: "$.io.ai",
			},
			Metrics: []config.Metric{{
				Name:      "metric_json",
				ValuePath: "$.aiValueScaled",
				Where: []config.Condition{{
					Path:   "$.aiIndex",
					Equals: 0,
				}},
				Transform: config.Transform{
					Scale: 1,
				},
			}},
		}},
	}

	samples, err := Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	if samples[0].ValueDouble == nil || *samples[0].ValueDouble != 1.5 {
		t.Fatalf("value_double = %#v, want 1.5", samples[0].ValueDouble)
	}
}

func TestRunInputXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<response><roll>53.2</roll><action>TRACK</action></response>`))
	}))
	defer server.Close()

	cfg := config.Config{
		Inputs: []config.Input{{
			Name: "xml_input",
			Request: config.Request{
				Method:  "GET",
				URL:     server.URL,
				Timeout: 5 * time.Second,
			},
			Response: config.Response{
				Format: "xml",
			},
			Metrics: []config.Metric{
				{
					Name:      "roll",
					ValuePath: "$.roll",
					Transform: config.Transform{
						Scale: 1,
					},
				},
				{
					Name:      "action",
					ValuePath: "$.action",
					Transform: config.Transform{
						Scale: 1,
					},
				},
			},
		}},
	}

	samples, err := Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("len(samples) = %d, want 2", len(samples))
	}
	if samples[0].ValueDouble == nil || *samples[0].ValueDouble != 53.2 {
		t.Fatalf("roll value_double = %#v, want 53.2", samples[0].ValueDouble)
	}
	if samples[1].ValueText == nil || *samples[1].ValueText != "TRACK" {
		t.Fatalf("action value_text = %#v, want TRACK", samples[1].ValueText)
	}
}
