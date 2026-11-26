package version

import "testing"

func TestIsSemver(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"0.1.0", true},
		{"1.0.0", true},
		{"10.20.30", true},
		{"1.0.0-rc.1", true},
		{"1.0.0-alpha+001", true},
		{"1.0.0+20130313144700", true},
		{"dev", false},
		{"", false},
		{"v1.0.0", false},
		{"1.0", false},
		{"1.0.0.0", false},
		{"01.0.0", false},
		{"1.0.0-", false},
		{"1.0.0-01", false},
	}
	for _, tt := range tests {
		if got := IsSemver(tt.in); got != tt.want {
			t.Errorf("IsSemver(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestIsRelease(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = "dev"
	if IsRelease() {
		t.Error("dev build must not be a release")
	}
	Version = "0.1.0"
	if !IsRelease() {
		t.Error("0.1.0 must be a release")
	}
}
