package decode

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func Payload(reader io.Reader, format string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		var payload any
		if err := json.NewDecoder(reader).Decode(&payload); err != nil {
			return nil, fmt.Errorf("decode json: %w", err)
		}
		return payload, nil
	case "xml":
		payload, err := decodeXML(reader)
		if err != nil {
			return nil, fmt.Errorf("decode xml: %w", err)
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("unsupported response format %q", format)
	}
}

type xmlNode struct {
	name     string
	children map[string]any
	text     strings.Builder
}

func decodeXML(reader io.Reader) (any, error) {
	decoder := xml.NewDecoder(reader)
	var stack []*xmlNode

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch tok := token.(type) {
		case xml.StartElement:
			stack = append(stack, &xmlNode{
				name:     tok.Name.Local,
				children: make(map[string]any),
			})
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			stack[len(stack)-1].text.Write(tok)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected closing tag %q", tok.Name.Local)
			}

			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			value := node.value()

			if len(stack) == 0 {
				return value, nil
			}

			parent := stack[len(stack)-1]
			mergeChild(parent.children, node.name, value)
		}
	}

	return nil, fmt.Errorf("xml document did not contain a root element")
}

func (n *xmlNode) value() any {
	if len(n.children) > 0 {
		return n.children
	}
	return parseScalar(strings.TrimSpace(n.text.String()))
}

func mergeChild(children map[string]any, key string, value any) {
	current, ok := children[key]
	if !ok {
		children[key] = value
		return
	}

	switch existing := current.(type) {
	case []any:
		children[key] = append(existing, value)
	default:
		children[key] = []any{existing, value}
	}
}

func parseScalar(text string) any {
	if text == "" {
		return ""
	}

	lower := strings.ToLower(text)
	if lower == "true" {
		return true
	}
	if lower == "false" {
		return false
	}

	if i, err := strconv.ParseInt(text, 10, 64); err == nil {
		return i
	}
	if u, err := strconv.ParseUint(text, 10, 64); err == nil {
		return u
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return f
	}

	return text
}

func ToJSON(value any) ([]byte, error) {
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}
