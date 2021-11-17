package performer

import "testing"

func TestEvent_Hash(t *testing.T) {
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
			if got := e.Hash(); got != tt.want {
				t.Errorf("Hash() = %v, want %v", got, tt.want)
			}
		})
	}
}
