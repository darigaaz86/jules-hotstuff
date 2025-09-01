package storage

import (
	"testing"
)

func TestNewMDBX(t *testing.T) {
	bc := NewMDBX(1)
	if bc == nil {
		t.Fatal("NewMDBX returned nil")
	}
}
