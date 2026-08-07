package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Atrabilis/api-poller/internal/config"
	"github.com/Atrabilis/api-poller/internal/cursor"
	"github.com/Atrabilis/api-poller/internal/decode"
	"github.com/Atrabilis/api-poller/internal/extract"
)

type Sample struct {
	InputName   string            `json:"input_name"`
	MetricName  string            `json:"metric_name"`
	ValueType   string            `json:"value_type"`
	ValueDouble *float64          `json:"value_double,omitempty"`
	ValueText   *string           `json:"value_text,omitempty"`
	ValueBool   *bool             `json:"value_bool,omitempty"`
	ValueJSON   json.RawMessage   `json:"value_json,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	TS          time.Time         `json:"ts"`
	SourceURL   string            `json:"source_url"`
}

// CursorUpdate is a candidate new cursor value produced by a poll. Callers
// should only persist it (cursor.Write) after any storage write for this
// run has succeeded, so a failed write can be retried instead of skipped.
type CursorUpdate struct {
	InputName string
	File      string
	Value     string
}

func Run(ctx context.Context, cfg config.Config, inputFilter string) ([]Sample, []CursorUpdate, error) {
	var samples []Sample
	var cursorUpdates []CursorUpdate

	for _, input := range cfg.Inputs {
		if inputFilter != "" && input.Name != inputFilter {
			continue
		}

		inputSamples, cursorUpdate, err := runInput(ctx, input)
		if err != nil {
			return nil, nil, fmt.Errorf("input %q: %w", input.Name, err)
		}
		samples = append(samples, inputSamples...)
		if cursorUpdate != nil {
			cursorUpdates = append(cursorUpdates, *cursorUpdate)
		}
	}

	if inputFilter != "" && len(samples) == 0 {
		return nil, nil, fmt.Errorf("input %q not found or produced no samples", inputFilter)
	}

	return samples, cursorUpdates, nil
}

func runInput(ctx context.Context, input config.Input) ([]Sample, *CursorUpdate, error) {
	reqCtx, cancel := context.WithTimeout(ctx, input.Request.Timeout)
	defer cancel()

	requestURL := input.Request.URL
	requestBody := input.Request.Body
	var cursorValue string
	if input.State != nil {
		var err error
		cursorValue, err = cursor.Read(input.State.File, input.State.InitialValue)
		if err != nil {
			return nil, nil, fmt.Errorf("read cursor: %w", err)
		}
		requestURL = strings.ReplaceAll(requestURL, input.State.Placeholder, cursorValue)
		requestBody = strings.ReplaceAll(requestBody, input.State.Placeholder, cursorValue)
	}

	var body io.Reader
	if requestBody != "" {
		body = strings.NewReader(requestBody)
	}

	req, err := http.NewRequestWithContext(reqCtx, input.Request.Method, requestURL, body)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	for key, value := range input.Request.Headers {
		req.Header.Set(key, expandEnv(value))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(errBody))
	}

	payload, err := decode.Payload(resp.Body, input.Response.Format)
	if err != nil {
		return nil, nil, err
	}

	items, err := extract.ResolveItems(payload, input.Response.ItemsPath)
	if err != nil {
		return nil, nil, err
	}

	observedAt := time.Now().UTC()
	var samples []Sample
	for _, metric := range input.Metrics {
		conditions := make([]extract.Condition, 0, len(metric.Where))
		for _, c := range metric.Where {
			conditions = append(conditions, extract.Condition{Path: c.Path, Equals: c.Equals})
		}

		for _, item := range items {
			match, err := extract.MatchesAll(item, conditions)
			if err != nil {
				return nil, nil, fmt.Errorf("metric %q filter: %w", metric.Name, err)
			}
			if !match {
				continue
			}

			rawValue, err := extract.Resolve(item, metric.ValuePath)
			if err != nil {
				return nil, nil, fmt.Errorf("metric %q value_path: %w", metric.Name, err)
			}

			sample, err := buildSample(input.Name, metric.Name, requestURL, observedAt, metric.Labels, rawValue, metric.Transform)
			if err != nil {
				return nil, nil, fmt.Errorf("metric %q build sample: %w", metric.Name, err)
			}
			samples = append(samples, sample)
			break
		}
	}

	var cursorUpdate *CursorUpdate
	if input.State != nil {
		newValue := cursorValue
		for _, item := range items {
			raw, err := extract.Resolve(item, input.State.CursorPath)
			if err != nil {
				continue
			}
			strValue, ok := cursor.Stringify(raw)
			if !ok {
				continue
			}
			newValue = cursor.Max(newValue, strValue)
		}
		if newValue != cursorValue {
			cursorUpdate = &CursorUpdate{InputName: input.Name, File: input.State.File, Value: newValue}
		}
	}

	return samples, cursorUpdate, nil
}

func buildSample(inputName, metricName, sourceURL string, observedAt time.Time, labels map[string]string, rawValue any, transform config.Transform) (Sample, error) {
	sample := Sample{
		InputName:  inputName,
		MetricName: metricName,
		Labels:     labels,
		TS:         observedAt,
		SourceURL:  sourceURL,
	}

	if number, err := extract.Numeric(rawValue); err == nil {
		value := number*transform.Scale + transform.Offset
		sample.ValueType = "double"
		sample.ValueDouble = &value
		return sample, nil
	}

	switch value := rawValue.(type) {
	case string:
		sample.ValueType = "text"
		sample.ValueText = &value
		return sample, nil
	case bool:
		sample.ValueType = "bool"
		sample.ValueBool = &value
		return sample, nil
	case nil:
		sample.ValueType = "json"
		sample.ValueJSON = json.RawMessage("null")
		return sample, nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return Sample{}, err
		}
		if scalarText, ok := stringifyScalar(value); ok {
			sample.ValueType = "text"
			sample.ValueText = &scalarText
			return sample, nil
		}
		sample.ValueType = "json"
		sample.ValueJSON = encoded
		return sample, nil
	}
}

func stringifyScalar(value any) (string, bool) {
	switch v := value.(type) {
	case int:
		return strconv.Itoa(v), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	default:
		return "", false
	}
}

// expandEnv resolves ${VAR_NAME} references in header values from the
// process environment, so secrets can live in the deployed .env file
// (same *_env indirection already used for storage credentials) instead of
// as plaintext in the committed YAML config.
func expandEnv(value string) string {
	return os.Expand(value, os.Getenv)
}
