package coursetips

import "testing"

func TestIsValidPECourse(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"体育选项课Ⅰ", true},
		{"体育选项课Ⅱ", true},
		{"体育选项课Ⅲ", true},
		{"体育选项课IV", false},
		{"高等数学", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsValidPECourse(tt.name); got != tt.want {
			t.Errorf("IsValidPECourse(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseScore(t *testing.T) {
	tests := []struct {
		input   string
		wantVal float64
		wantOk  bool
	}{
		{"85", 85, true},
		{"60.5", 60.5, true},
		{"0", 0, true},
		{"100", 100, true},
		{"优", 95, true},
		{"良", 85, true},
		{"中", 75, true},
		{"及格", 65, true},
		{"不及格", 50, true},
		{"", 0, false},
		{"abc", 0, false},
		{"未知", 0, false},
	}
	for _, tt := range tests {
		val, ok := ParseScore(tt.input)
		if val != tt.wantVal || ok != tt.wantOk {
			t.Errorf("ParseScore(%q) = (%v, %v), want (%v, %v)", tt.input, val, ok, tt.wantVal, tt.wantOk)
		}
	}
}
