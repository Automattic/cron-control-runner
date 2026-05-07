package performer

import "testing"

func TestEvent_LockKey(t *testing.T) {
	type fields struct {
		URL       string
		Timestamp int
		Action    string
		Instance  string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name: "hello",
			fields: fields{
				URL:       "https://foo.bar",
				Timestamp: 12345,
				Action:    "yolo",
				Instance:  "daddy",
			},
			want: "WQQ5XKF6R4C6VWN3WYKFJQFGLMHDFBC2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Event{
				URL:       tt.fields.URL,
				Timestamp: tt.fields.Timestamp,
				Action:    tt.fields.Action,
				Instance:  tt.fields.Instance,
			}
			if got := e.LockKey(); got != tt.want {
				t.Errorf("LockKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeJSONInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain json unchanged",
			input: `[{"url":"https://example.com"}]`,
			want:  `[{"url":"https://example.com"}]`,
		},
		{
			name:  "strips bom",
			input: "\uFEFF[{\"url\":\"https://example.com\"}]",
			want:  `[{"url":"https://example.com"}]`,
		},
		{
			name:  "strips leading whitespace",
			input: " \n\t[{\"url\":\"https://example.com\"}]",
			want:  `[{"url":"https://example.com"}]`,
		},
		{
			name:  "strips whitespace then bom",
			input: " \n\t\uFEFF[{\"url\":\"https://example.com\"}]",
			want:  `[{"url":"https://example.com"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeJSONInput(tt.input)
			if got != tt.want {
				t.Fatalf("sanitizeJSONInput() = %q, want %q", got, tt.want)
			}
		})
	}
}
