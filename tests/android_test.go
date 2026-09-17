package tests

import (
	"testing"

	"github.com/debaucheryparty/packets/internal/android"
)

func TestAndroid_ClassifyDevice(t *testing.T) {
	tests := []struct {
		serial     string
		state      string
		wantType   android.DeviceType
		wantStatus string
	}{
		{"Pixel-remote", "device", android.DeviceTypeNetwork, "ready"},
		{"192.168.1.55:5555", "device", android.DeviceTypeNetwork, "ready"},
		{"remote-device-1", "offline", android.DeviceTypeNetwork, "offline"},
		{"emulator-5554", "device", android.DeviceTypeEmulator, "ready"},
		{"emulator-01", "device", android.DeviceTypeEmulator, "ready"},
		{"localhost:5555", "device", android.DeviceTypeEmulator, "ready"},
		{"127.0.0.1:5555", "device", android.DeviceTypeEmulator, "ready"},
		{"HT7491A00123", "device", android.DeviceTypeUSB, "ready"},
		{"HT7491A00123", "unauthorized", android.DeviceTypeUSB, "unauthorized"},
	}

	for _, tt := range tests {
		dev := android.ClassifyDevice(tt.serial, tt.state)
		if dev.Type != tt.wantType {
			t.Errorf("ClassifyDevice(%q, %q).Type = %q, want %q", tt.serial, tt.state, dev.Type, tt.wantType)
		}
		if dev.Status != tt.wantStatus {
			t.Errorf("ClassifyDevice(%q, %q).Status = %q, want %q", tt.serial, tt.state, dev.Status, tt.wantStatus)
		}
	}
}

func TestAndroid_SessionManager(t *testing.T) {
	sm := android.NewSessionManager()
	s := sm.CreateSession("node-1", "Pixel_8_API_34")
	if s == nil {
		t.Fatal("expected non-nil session")
	}
	if s.State != android.SessionStateCreating {
		t.Errorf("expected state %s, got %s", android.SessionStateCreating, s.State)
	}

	found, ok := sm.GetSession(s.ID)
	if !ok || found.ID != s.ID {
		t.Fatalf("failed to retrieve created session: %s", s.ID)
	}

	sm.TouchSession(s.ID)
	list := sm.ListSessions()
	if len(list) != 1 {
		t.Errorf("expected 1 session, got %d", len(list))
	}

	sm.CloseSession(s.ID)
	if found.State != android.SessionStateStopped {
		t.Errorf("expected state %s, got %s", android.SessionStateStopped, found.State)
	}
}
