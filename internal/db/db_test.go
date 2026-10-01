package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitDB(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agy_test_db_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	if target := GetHandoffTarget(database); target != 150000 {
		t.Errorf("expected default handoff target 150000, got %d", target)
	}

	if flag := GetManualHandoffFlag(database); flag != false {
		t.Errorf("expected default manual flag false, got %v", flag)
	}

	err = SetSystemConfig(database, "handoff_target", "140000")
	if err != nil {
		t.Fatalf("SetSystemConfig failed: %v", err)
	}

	if target := GetHandoffTarget(database); target != 140000 {
		t.Errorf("expected updated handoff target 140000, got %d", target)
	}
}
