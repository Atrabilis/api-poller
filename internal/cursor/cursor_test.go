package cursor

import (
	"path/filepath"
	"testing"
)

func TestReadMissingFileReturnsInitialValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	value, err := Read(path, "0")
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if value != "0" {
		t.Fatalf("value = %q, want %q", value, "0")
	}
}

func TestWriteThenRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cursor.json")

	if err := Write(path, "278474498"); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	value, err := Read(path, "0")
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if value != "278474498" {
		t.Fatalf("value = %q, want %q", value, "278474498")
	}
}

func TestMaxNumeric(t *testing.T) {
	if got := Max("5", "10"); got != "10" {
		t.Fatalf("Max(5, 10) = %q, want 10", got)
	}
	if got := Max("10", "5"); got != "10" {
		t.Fatalf("Max(10, 5) = %q, want 10", got)
	}
}

func TestMaxLexicographicFallback(t *testing.T) {
	got := Max("2026-08-01 00:00:00", "2026-08-04 17:52:52")
	if got != "2026-08-04 17:52:52" {
		t.Fatalf("Max returned %q, want the later timestamp", got)
	}
}

func TestMaxEmptyOperand(t *testing.T) {
	if got := Max("", "5"); got != "5" {
		t.Fatalf("Max(\"\", 5) = %q, want 5", got)
	}
	if got := Max("5", ""); got != "5" {
		t.Fatalf("Max(5, \"\") = %q, want 5", got)
	}
}
